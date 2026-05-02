#!/usr/bin/env bash
set -euo pipefail

# Integration tests for Forge server API
# Usage: ./scripts/integration_test.sh [-o output.txt] [--skip pattern] [model1 model2 ...]
# If no models specified, auto-discovers from Ollama.

OUTPUT_FILE=""
SKIP_PATTERN=""
MODELS_ARGS=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o|--output) OUTPUT_FILE="$2"; shift 2 ;;
        --skip) SKIP_PATTERN="$2"; shift 2 ;;
        *) MODELS_ARGS+=("$1"); shift ;;
    esac
done
set -- "${MODELS_ARGS[@]+"${MODELS_ARGS[@]}"}"

# If output file specified, tee all output (strip ANSI codes in file, flush each line)
if [ -n "$OUTPUT_FILE" ]; then
    mkdir -p "$(dirname "$OUTPUT_FILE")"
    : > "$OUTPUT_FILE"
    exec > >(while IFS= read -r line; do
        printf '%s\n' "$line"
        printf '%s\n' "$line" | sed 's/\x1b\[[0-9;]*m//g' >> "$OUTPUT_FILE"
    done) 2>&1
fi

PORT="${FORGE_PORT:-8080}"
BASE="http://localhost:${PORT}"
TIMEOUT="${FORGE_TIMEOUT:-180}"
PASS=0
FAIL=0
SKIP=0
RESULTS=()

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
DIM='\033[2m'
BOLD='\033[1m'
NC='\033[0m'

log()  { echo -e "  ${CYAN}●${NC} $*"; }
pass() { echo -e "  ${GREEN}✓${NC} $*"; PASS=$((PASS+1)); RESULTS+=("PASS: $*"); }
fail() { echo -e "  ${RED}✗${NC} $*"; FAIL=$((FAIL+1)); RESULTS+=("FAIL: $*"); }
skip() { echo -e "  ${YELLOW}○${NC} $* ${DIM}(skipped)${NC}"; SKIP=$((SKIP+1)); RESULTS+=("SKIP: $*"); }

# POST /v1/chat and return the JSON response
chat() {
    local msg="$1"
    local model="${2:-}"
    local body
    if [ -n "$model" ]; then
        body=$(printf '{"message":%s,"model":%s}' "$(json_str "$msg")" "$(json_str "$model")")
    else
        body=$(printf '{"message":%s}' "$(json_str "$msg")")
    fi
    curl -s --max-time "$TIMEOUT" "$BASE/v1/chat" \
        -H 'Content-Type: application/json' \
        -d "$body" 2>/dev/null
}

json_str() { python3 -c "import json; print(json.dumps('$1'))"; }

# Check if a tool was used in the response
has_tool() {
    local resp="$1" tool="$2"
    echo "$resp" | python3 -c "
import sys,json
r=json.load(sys.stdin)
sys.exit(0 if any(e.get('tool')=='$tool' for e in r.get('events',[])) else 1)
" 2>/dev/null
}

# Check if response has a tool error
has_tool_error() {
    local resp="$1" pattern="$2"
    echo "$resp" | python3 -c "
import sys,json
r=json.load(sys.stdin)
sys.exit(0 if any('$pattern' in str(e) for e in r.get('events',[])) else 1)
" 2>/dev/null
}

# Check if response has text
has_text() {
    local resp="$1"
    echo "$resp" | python3 -c "
import sys,json
r=json.load(sys.stdin)
sys.exit(0 if any(e.get('type')=='text' for e in r.get('events',[])) else 1)
" 2>/dev/null
}

event_summary() {
    local resp="$1"
    echo "$resp" | python3 -c "
import sys,json
r=json.load(sys.stdin)
for e in r.get('events',[]):
    t=e.get('type','')
    if t.startswith('tool'):
        print(f'    {t}: {e.get(\"tool\",\"\")} {e.get(\"detail\",\"\")} {e.get(\"error\",\"\")}')
    elif t=='text':
        txt=e.get('text','')[:100].replace(chr(10),' ')
        print(f'    text: {txt}...')
    elif t=='error':
        print(f'    error: {e.get(\"error\",\"\")}')
" 2>/dev/null
}

# ─── Health check ───────────────────────────────────────────────────────────

check_server() {
    log "Checking server at $BASE..."
    local resp
    resp=$(curl -s --max-time 5 "$BASE/health" 2>/dev/null || true)
    if echo "$resp" | grep -q '"ok"'; then
        pass "Server is healthy"
        return 0
    else
        fail "Server not reachable at $BASE"
        echo -e "  ${DIM}Start it with: forge serve --port $PORT${NC}"
        return 1
    fi
}

# ─── Test functions ─────────────────────────────────────────────────────────

test_health() {
    local status
    status=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/health")
    [ "$status" = "200" ] && pass "GET /health → 200" || fail "GET /health → $status (expected 200)"
}

test_empty_message() {
    local status
    status=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/v1/chat" \
        -H 'Content-Type: application/json' -d '{"message": ""}')
    [ "$status" = "400" ] && pass "Empty message → 400" || fail "Empty message → $status (expected 400)"
}

test_bad_json() {
    local status
    status=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/v1/chat" \
        -H 'Content-Type: application/json' -d 'not json')
    [ "$status" = "400" ] && pass "Bad JSON → 400" || fail "Bad JSON → $status (expected 400)"
}

test_list_directory() {
    local model="${1:-}" label="${2:-default}"
    log "[$label] list_directory..."
    local resp
    resp=$(chat "list internal/" "$model")
    if has_tool "$resp" "list_directory"; then
        pass "[$label] list_directory"
    else
        fail "[$label] list_directory — tool not called"
    fi
    event_summary "$resp"
}

test_read_file() {
    local model="${1:-}" label="${2:-default}"
    log "[$label] read_file..."
    local resp
    resp=$(chat "read go.mod" "$model")
    if has_tool "$resp" "read_file"; then
        pass "[$label] read_file"
    else
        fail "[$label] read_file — tool not called"
    fi
    event_summary "$resp"
}

test_shell_exec() {
    local model="${1:-}" label="${2:-default}"
    log "[$label] shell_exec..."
    local resp
    resp=$(chat "run: echo hello" "$model")
    if has_tool "$resp" "shell_exec"; then
        pass "[$label] shell_exec"
    else
        fail "[$label] shell_exec — tool not called"
    fi
    event_summary "$resp"
}

test_search_code() {
    local model="${1:-}" label="${2:-default}"
    log "[$label] search_code..."
    local resp
    resp=$(chat 'search for "func New" in *.go files' "$model")
    if has_tool "$resp" "search_code"; then
        pass "[$label] search_code"
    else
        fail "[$label] search_code — tool not called"
    fi
    event_summary "$resp"
}

test_project_boundary() {
    local model="${1:-}" label="${2:-default}"
    log "[$label] project boundary..."
    local resp
    resp=$(chat "read /etc/passwd" "$model")
    if has_tool_error "$resp" "outside project"; then
        pass "[$label] project boundary — blocked"
    elif has_tool "$resp" "read_file"; then
        fail "[$label] project boundary — read_file called but not blocked"
    else
        skip "[$label] project boundary — model didn't attempt"
    fi
    event_summary "$resp"
}

test_multi_tool() {
    local model="${1:-}" label="${2:-default}"
    log "[$label] multi-tool chain..."
    local resp
    resp=$(chat "list internal/ then read go.mod" "$model")
    local tool_count
    tool_count=$(echo "$resp" | python3 -c "
import sys,json
r=json.load(sys.stdin)
print(sum(1 for e in r.get('events',[]) if e.get('type')=='tool_start'))
" 2>/dev/null)
    if [ "$tool_count" -ge 2 ] 2>/dev/null; then
        pass "[$label] multi-tool chain ($tool_count tools)"
    else
        fail "[$label] multi-tool chain — only $tool_count tool(s)"
    fi
    event_summary "$resp"
}

# ─── Run tests for a model ──────────────────────────────────────────────────

run_model_tests() {
    local model="${1:-}" label="${2:-default}"
    echo ""
    echo -e "${BOLD}─── Testing: $label ───${NC}"
    test_list_directory "$model" "$label"
    test_read_file "$model" "$label"
    test_shell_exec "$model" "$label"
    test_search_code "$model" "$label"
    test_project_boundary "$model" "$label"
    test_multi_tool "$model" "$label"
}

# ─── Main ───────────────────────────────────────────────────────────────────

main() {
    echo -e "${BOLD}Forge Integration Tests${NC}"
    echo -e "${DIM}Server: $BASE | Timeout: ${TIMEOUT}s${NC}"
    echo ""

    check_server || exit 1

    # Protocol tests (model-independent)
    echo ""
    echo -e "${BOLD}─── Protocol Tests ───${NC}"
    test_health
    test_empty_message
    test_bad_json

    # Model tests
    local models=("$@")
    if [ ${#models[@]} -eq 0 ]; then
        # Auto-discover models from Ollama
        log "No models specified, discovering from Ollama..."
        while IFS= read -r m; do
            models+=("$m")
        done < <(curl -s http://localhost:11434/api/tags 2>/dev/null | python3 -c "
import sys,json
try:
    tags=json.load(sys.stdin)
    for m in tags.get('models',[]):
        print(m['name'])
except: pass
" 2>/dev/null)
        if [ ${#models[@]} -eq 0 ]; then
            log "Could not discover models, testing server default only"
            run_model_tests "" "default"
        else
            log "Found ${#models[@]} model(s): ${models[*]}"
            for model in "${models[@]}"; do
                if [ -n "$SKIP_PATTERN" ] && [[ "$model" == *"$SKIP_PATTERN"* ]]; then
                    skip "[$model] skipped (matches --skip '$SKIP_PATTERN')"
                    continue
                fi
                run_model_tests "$model" "$model"
            done
        fi
    else
        for model in "${models[@]}"; do
            if [ -n "$SKIP_PATTERN" ] && [[ "$model" == *"$SKIP_PATTERN"* ]]; then
                skip "[$model] skipped (matches --skip '$SKIP_PATTERN')"
                continue
            fi
            run_model_tests "$model" "$model"
        done
    fi

    # Summary
    echo ""
    echo -e "${BOLD}─── Summary ───${NC}"
    local total=$((PASS+FAIL+SKIP))
    echo -e "  ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}, ${YELLOW}$SKIP skipped${NC} / $total total"
    echo ""
    if [ "$FAIL" -gt 0 ]; then
        echo -e "${RED}Failures:${NC}"
        for r in "${RESULTS[@]}"; do
            if [[ "$r" == FAIL* ]]; then echo "  $r"; fi
        done
        echo ""
        exit 1
    fi
}

main "$@"
