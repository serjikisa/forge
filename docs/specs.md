# Forge — Technical Specification

## Overview

Forge is a terminal-based AI coding agent built in Go. It connects to local LLMs (via Ollama) and cloud LLM providers to assist developers with coding tasks, file operations, and system management directly from the command line.

Inspired by tools like Kiro — Forge aims to be the best-in-class terminal AI agent: fast, beautiful, and concurrent.

## Principles

- **Standard library only** — No third-party dependencies. All functionality built with Go's standard library.
- **Concurrency first** — Leverage goroutines and channels throughout. Parallel tool execution, non-blocking streaming, background indexing.
- **Beautiful terminal UX** — Rich ANSI colors, spinners, progress indicators, and a polished prompt. The terminal should feel alive.
- **Speed** — Sub-second startup. Instant response streaming. No lag between thought and action.

## Goals

- Single static binary, zero runtime dependencies
- Provider-agnostic LLM integration (local and cloud)
- Agentic tool use (file I/O, shell execution, code search)
- Concurrent tool execution for independent operations
- Rich, colorful terminal experience inspired by modern CLI tools
- Extensible architecture for adding new providers and tools

## Architecture

```
  +--------------------------------------------------+
  |              Terminal UI Layer                    |
  |  [Prompt Engine] [Spinner/Status] [Syntax/MD]     |
  +--------------------------------------------------+
  |              Agent Orchestrator                   |
  |  [Concurrent Tool Executor via goroutines]        |
  +--------------------------------------------------+
  |              Provider Layer                       |
  |  Ollama | OpenAI | Anthropic | Bedrock | ...      |
  +--------------------------------------------------+
  |              Context & State                      |
  |  Sessions | History | File Index | Config         |
  +--------------------------------------------------+
```

## Components

### 1. CLI Entry Point

- Framework: standard library (`flag` + `os.Args`)
- Commands:
  - `forge chat` — interactive chat session
  - `forge ask "<prompt>"` — single-shot query
  - `forge models` — list available models for the active provider
  - `forge config` — manage provider settings
  - `forge update` — self-update to latest version
  - `forge version` — print version info

### 2. Model Selection

Precedence order (highest wins):

```
CLI flag → Environment variable → Config file → Built-in default
```

- **Config file** — sets the default provider and model (`~/.forge/config.json`)
- **CLI flags** — override per session (`--provider`, `--model`)
- **Environment variables** — for CI/scripting (`FORGE_PROVIDER`, `FORGE_MODEL`)
- **Interactive picker** — on first run or via `forge config`, arrow-key selection with colored highlights
- **Ollama auto-discovery** — queries `GET /api/tags` to list locally installed models

### 3. LLM Provider Interface

```go
type Provider interface {
    ChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
    ListModels(ctx context.Context) ([]Model, error)
    Name() string
    SupportsToolCalling() bool
}
```

**Core types:**

```go
type ChatRequest struct {
    Messages []Message
    Tools    []Tool
    Stream   bool
}

type ChatEvent struct {
    Type       EventType       // EventText, EventToolCall, EventError, EventDone
    Text       string          // for EventText: token delta
    ToolCall   *ToolCall       // for EventToolCall: tool invocation
    Error      error           // for EventError
}

type EventType int
const (
    EventText     EventType = iota
    EventToolCall
    EventError
    EventDone
)

type Message struct {
    Role       string          // "system", "user", "assistant", "tool"
    Content    string          // text content
    ToolCalls  []ToolCall      // assistant requesting tool use
    ToolCallID string          // for tool result messages
}

type ToolCall struct {
    ID        string          // provider-assigned ID (e.g. "call_abc123")
    Name      string          // tool name (e.g. "read_file")
    Arguments json.RawMessage // JSON parameters
}

type ToolResult struct {
    ToolCallID string
    Content    string
    Error      error           // non-nil if tool execution failed or panicked
}

type Model struct {
    ID          string
    Name        string
    ContextSize int            // max tokens (0 = unknown)
}
```

Supported providers:
- **Ollama** — local models (Llama, Qwen, Mistral, etc.)
- **OpenAI** — GPT-4, GPT-4o, etc.
- **Anthropic** — Claude models
- **AWS Bedrock** — managed cloud models
- **DeepSeek** — DeepSeek-V3, DeepSeek-R1, etc. (OpenAI-compatible API)
- **Alibaba Cloud (DashScope)** — Qwen models via DashScope API

DeepSeek reuses the OpenAI provider with a different base URL.

All providers implement streaming via `net/http` and `encoding/json`.

**HTTP client discipline** — never use `http.DefaultClient`:

```go
var providerClient = &http.Client{
    Timeout: 120 * time.Second, // long for streaming responses
    Transport: &http.Transport{
        MaxIdleConns:        20,
        MaxIdleConnsPerHost: 10,
        IdleConnTimeout:     90 * time.Second,
    },
}
```

- Every request uses `http.NewRequestWithContext(ctx, ...)` for cancellation support
- `defer resp.Body.Close()` on every response — unclosed bodies leak connections
- `io.LimitReader(resp.Body, 10<<20)` on non-streaming responses to cap memory usage
- Retry with exponential backoff for transient failures (429, 503):

```go
func fetchWithRetry(ctx context.Context, client *http.Client, req *http.Request, maxRetries int) (*http.Response, error) {
    var lastErr error
    for i := range maxRetries {
        resp, err := client.Do(req)
        if err == nil && resp.StatusCode < 500 && resp.StatusCode != 429 {
            return resp, nil
        }
        if resp != nil {
            resp.Body.Close()
        }
        lastErr = err
        select {
        case <-time.After(time.Duration(i*i) * 100 * time.Millisecond):
        case <-ctx.Done():
            return nil, ctx.Err()
        }
    }
    return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}
```

### 4. Tool System

```go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Execute(ctx context.Context, params json.RawMessage) (string, error)
    SafetyLevel() SafetyLevel  // Safe, NeedsConfirmation, Dangerous
}

type SafetyLevel int
const (
    Safe             SafetyLevel = iota  // read_file, list_directory
    NeedsConfirmation                     // write_file, shell_exec
    Dangerous                             // rm -rf, git push --force
)
```

Built-in tools:
- `read_file` — read file contents
- `write_file` — create or edit files
- `list_directory` — list directory contents
- `shell_exec` — execute shell commands
- `search_code` — regex search across codebase
- `web_search` — search the web via configurable provider (SearXNG self-hosted, or Brave/Google API). Uses `net/http` — fully stdlib compatible. Disabled by default; enable by setting `search_provider` in config.

### 5. Concurrent Execution Engine

The core differentiator. When the LLM requests multiple tool calls, Forge runs them concurrently with bounded parallelism and panic safety.

**safeGo helper — panic isolation:**

A panic in any tool goroutine must not crash the entire session. All goroutine launches go through `safeGo`:

```go
func safeGo(fn func()) {
    go func() {
        defer func() {
            if r := recover(); r != nil {
                slog.Error("goroutine panic", "recover", r, "stack", string(debug.Stack()))
            }
        }()
        fn()
    }()
}
```

**Bounded concurrent tool execution:**

Use `SetLimit` to cap parallel tool calls. Prevents resource exhaustion (too many open files, too many shell processes):

```go
func (e *Executor) RunTools(ctx context.Context, calls []ToolCall) []ToolResult {
    results := make([]ToolResult, len(calls))
    var wg sync.WaitGroup
    sem := make(chan struct{}, e.maxConcurrency) // default: 5

    for i, call := range calls {
        wg.Add(1)
        sem <- struct{}{} // acquire
        go func() {
            defer wg.Done()
            defer func() { <-sem }() // release
            defer func() {
                if r := recover(); r != nil {
                    slog.Error("tool panic", "tool", call.Name, "recover", r)
                    results[i] = ToolResult{
                        ToolCallID: call.ID,
                        Error:      fmt.Errorf("tool %s panicked: %v", call.Name, r),
                    }
                }
            }()
            results[i] = e.executeTool(ctx, call)
        }()
    }
    wg.Wait()
    return results
}
```

Note: Each goroutine writes to a distinct index (`results[i]`), so no mutex is needed. This is safe because no two goroutines share an index. Requires Go 1.22+ (loop variable capture fix).

**Goroutine leak prevention:**

Every goroutine that reads from a channel or waits on I/O must have a cancellation path via `select` on `ctx.Done()`. This is mandatory — the GC does NOT collect blocked goroutines.

```go
// WRONG — leaks if ctx is cancelled while waiting on ch
result := <-ch

// RIGHT — exits cleanly on cancellation
select {
case result := <-ch:
    // process
case <-ctx.Done():
    return ctx.Err()
}
```

All pipeline stages (streaming, indexing, tool execution) follow this pattern.

**Rate limiting for external API calls:**

Shared rate limiter per provider to stay within API quotas. Uses `time.Ticker` for simple token-bucket behavior (stdlib only):

```go
type RateLimiter struct {
    ticker *time.Ticker
    tokens chan struct{}
    done   chan struct{}
}

func NewRateLimiter(rps int) *RateLimiter {
    rl := &RateLimiter{
        ticker: time.NewTicker(time.Second / time.Duration(rps)),
        tokens: make(chan struct{}, rps),
        done:   make(chan struct{}),
    }
    safeGo(func() {
        for {
            select {
            case <-rl.ticker.C:
                select {
                case rl.tokens <- struct{}{}:
                default: // bucket full
                }
            case <-rl.done:
                rl.ticker.Stop()
                return
            }
        }
    })
    return rl
}

func (rl *RateLimiter) Wait(ctx context.Context) error {
    select {
    case <-rl.tokens:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

func (rl *RateLimiter) Close() {
    close(rl.done)
}
```

Dynamic adjustment: slow down on 429 responses, speed up when healthy.

Concurrency is used throughout:
- **Parallel tool execution** — bounded by semaphore, panic-safe via `safeGo`
- **Streaming + UI** — response streaming and UI rendering run on separate goroutines
- **Background file indexing** — codebase scanning in background goroutines on startup
- **Provider health checks** — check multiple provider endpoints concurrently
- **Session auto-save** — periodic saves via background goroutine, no blocking the main loop
- **Rate limiting** — shared per-provider limiter for all outbound API calls
- **Cancellation** — `context.Context` propagation with `select` on `ctx.Done()` in every goroutine

### 6. Agent Loop

1. User sends a message
2. Agent builds prompt with system instructions, conversation history, and tool definitions
3. LLM responds with text and/or tool calls (streamed via channel)
4. If tool calls: execute concurrently, show live status per tool, append results, go to step 3
5. If text only: stream response to user with syntax highlighting

### 7. Context Management

- Conversation history stored in memory per session
- Persistent sessions saved to `~/.forge/sessions/` as JSON
- File contents included via tool results (not pre-loaded)
- Context window tracking to avoid exceeding model limits
- Background goroutine for session auto-save

### 8. Configuration

Config file: `~/.forge/config.json`

Environment variables in values (e.g. `"${OPENAI_API_KEY}"`) are expanded at load time via `os.ExpandEnv`. This keeps secrets out of the config file.

**Required vs optional config values:**

```go
// mustGetEnv fails fast at startup if a required value is missing.
// Uses os.LookupEnv to distinguish "not set" from "set to empty".
func mustGetEnv(key string) string {
    val, ok := os.LookupEnv(key)
    if !ok {
        log.Fatalf("required environment variable %s is not set", key)
    }
    return val
}

// getEnv returns a fallback for optional values.
func getEnv(key, fallback string) string {
    if val, ok := os.LookupEnv(key); ok {
        return val
    }
    return fallback
}
```

Use `mustGetEnv` for API keys when a cloud provider is selected. Use `getEnv` with defaults for optional settings (log level, theme, timeouts).

Local storage layout:

```
~/.forge/
├── config.json          ← user configuration
├── state.json           ← runtime state (last update check, session metadata)
└── sessions/            ← persisted conversation sessions
    ├── 2026-05-01_14-30.json
    └── ...
```

`state.json` tracks ephemeral runtime data:

```json
{
  "last_update_check": "2026-05-01T10:00:00Z",
  "latest_known_version": "v1.4.0",
  "last_session_id": "2026-05-01_14-30"
}
```

```json
{
  "default_provider": "ollama",
  "theme": "vibrant",
  "log_level": "info",
  "log_format": "text",
  "auto_update": true,
  "max_concurrency": 5,
  "providers": {
    "ollama": {
      "host": "http://localhost:11434",
      "model": "llama3"
    },
    "openai": {
      "base_url": "https://api.openai.com/v1",
      "api_key": "${OPENAI_API_KEY}",
      "model": "gpt-4o"
    },
    "anthropic": {
      "api_key": "${ANTHROPIC_API_KEY}",
      "model": "claude-sonnet-4-20250514"
    },
    "bedrock": {
      "region": "us-east-1",
      "model": "anthropic.claude-sonnet-4-20250514-v1:0"
    },
    "deepseek": {
      "base_url": "https://api.deepseek.com/v1",
      "api_key": "${DEEPSEEK_API_KEY}",
      "model": "deepseek-v3"
    },
    "alibaba": {
      "base_url": "https://dashscope.aliyuncs.com/api/v1",
      "api_key": "${DASHSCOPE_API_KEY}",
      "model": "qwen-max"
    }
  }
}
```

### 9. Terminal UI & Color System

A rich, Kiro-inspired terminal experience:

```
+--- Color Palette -----------------------------------+
|                                                     |
|  🔵 Cyan      -- user prompt, input caret           |
|  🟢 Green     -- success, tool completion           |
|  🟡 Yellow    -- warnings, confirmations            |
|  🔴 Red       -- errors, dangerous actions          |
|  🟣 Magenta   -- model name, provider info          |
|  ⚪ Dim gray  -- timestamps, metadata               |
|  🔷 Bold cyan -- forge branding, headers            |
|                                                     |
+-----------------------------------------------------+
```

**Prompt design:**
```
  ⚡ forge • ollama/llama3
  ❯ _
```

**Streaming response with tool use:**
```
  ⚡ forge • ollama/llama3
  ❯ fix the bug in main.go

  ◐ Reading main.go...
  ◑ Searching for related tests...
  ✓ read_file main.go (23 lines)
  ✓ search_code "TestMain" (2 matches)

  I found the issue on line 14. The error is...

  ◐ Writing fix to main.go...
  ✓ write_file main.go (patched line 14)

  Fixed. The nil pointer was caused by...
```

**Features:**
- Animated spinners (◐ ◑ ◒ ◓) for in-progress operations via goroutine
- Color-coded tool status (spinning = yellow, done = green, error = red)
- Syntax highlighting for code blocks using ANSI colors
- Markdown rendering (bold, italic, headers, lists, code) via ANSI
- Branded prompt with provider/model info
- Responsive — streaming tokens appear instantly, no buffering
- Terminal width detection for proper text wrapping:
  - Unix/macOS: `TIOCGWINSZ` ioctl via `syscall`
  - Windows: `GetConsoleScreenBufferInfo` via `syscall`

### 10. Themes

```json
{
  "theme": "vibrant"
}
```

Built-in themes:
- `vibrant` — full color, bold accents (default)
- `minimal` — subtle colors, clean look
- `mono` — no color, for piping output or accessibility

## Logging

Structured logging via `log/slog` (stdlib since Go 1.21). Configured once at startup.

```go
func setupLogger(level, format string) {
    var lvl slog.Level
    switch level {
    case "debug":
        lvl = slog.LevelDebug
    case "warn":
        lvl = slog.LevelWarn
    case "error":
        lvl = slog.LevelError
    default:
        lvl = slog.LevelInfo
    }

    opts := &slog.HandlerOptions{Level: lvl}
    var handler slog.Handler
    if format == "json" {
        handler = slog.NewJSONHandler(os.Stderr, opts)
    } else {
        handler = slog.NewTextHandler(os.Stderr, opts)
    }
    slog.SetDefault(slog.New(handler))
}
```

Usage:
- `slog.Info("tool executed", "tool", name, "duration", elapsed)` — structured key-value pairs
- `slog.With("session", sessionID)` — attach persistent fields for a session
- `slog.Error("api call failed", "provider", name, "err", err)` — errors with context
- Logs go to `stderr` so they don't interfere with `stdout` output
- Text format for development, JSON for production/debugging (`--log-format json`)
- Log level configurable via `--log-level debug` or `FORGE_LOG_LEVEL`

## Permission System

Forge uses a granular permission model. The user controls what the agent can do.

### Permission Levels

```go
type Permission int
const (
    Ask    Permission = iota  // prompt user every time
    Allow                      // auto-approve for this session
    Deny                       // block entirely
)
```

### Permission Categories

| Category | Default | What it covers |
|----------|---------|----------------|
| `file_read` | `Allow` | `read_file`, `list_directory`, `search_code` |
| `file_write` | `Ask` | `write_file` (shows diff, asks confirmation) |
| `shell` | `Ask` | `shell_exec` (shows command, asks confirmation) |
| `web` | `Ask` | `web_search` (shows query, asks confirmation) |
| `external` | `Ask` | User-defined external tools (see below) |

Configurable in `config.json`:
```json
{
  "permissions": {
    "file_read": "allow",
    "file_write": "ask",
    "shell": "ask",
    "web": "ask",
    "external": "ask"
  }
}
```

Override per session: `forge chat --allow-write --allow-shell` auto-approves for that session.

### Permission Prompt UX

```
  ⚡ forge • ollama/llama3

  🔒 shell_exec wants to run:
     git status
  [y]es / [n]o / [a]llow all shell for this session > _
```

The `a` option sets that category to `Allow` for the rest of the session.

### External Tools

Users can register external tools — any executable that accepts JSON on stdin and returns JSON on stdout:

```json
{
  "external_tools": [
    {
      "name": "deploy",
      "description": "Deploy the current project to staging",
      "command": "./scripts/deploy.sh",
      "schema": { "type": "object", "properties": { "env": { "type": "string" } } },
      "permission": "ask"
    }
  ]
}
```

External tools always require permission by default. The agent sees them alongside built-in tools and can invoke them when appropriate.

### Web Search Permission

When `web_search` is enabled and the agent wants to search:

```
  🔒 web_search wants to query:
     "Go ANSI escape codes terminal"
  [y]es / [n]o / [a]llow all web for this session > _
```

The user sees the exact query before it's sent. No web requests happen without explicit approval (unless `web` permission is set to `allow`).

## User Interrupt — Sending Prompts While Agent is Working

The user can type and send messages while the agent is actively streaming a response or executing tools. This enables:

- **Steering:** "actually, skip that file and look at handler.go instead"
- **Cancelling:** "stop" or Ctrl+C to cancel the current operation
- **Adding context:** "also check the test file" while the agent is mid-thought

### How It Works

```
  +------------------+          +------------------+
  | Input goroutine  |  ------> | Interrupt channel |
  | (always reading) |          +------------------+
  +------------------+                   |
                                         v
                              +---------------------+
                              | Agent loop checks   |
                              | interrupt channel    |
                              | between tool calls   |
                              +---------------------+
```

- A dedicated input goroutine reads from stdin at all times, even during agent execution
- User messages go into a buffered interrupt channel
- The agent checks the interrupt channel:
  - **Between tool calls** — before executing the next tool batch
  - **After streaming completes** — before prompting for next input
- If an interrupt message is found, the agent:
  1. Appends the user's new message to conversation history
  2. Cancels any in-flight tool executions (via context)
  3. Sends the updated history to the LLM for a new response

### UX

```
  ⚡ forge • ollama/llama3
  ❯ refactor the auth module

  ◐ Reading auth.go...
  ◐ Reading auth_test.go...

  ❯ actually focus on the middleware, not auth     <-- user types while agent works

  ⚠ Interrupted — redirecting to middleware...
  ◐ Reading middleware.go...
```

### Implementation

```go
type Agent struct {
    interrupt chan string  // buffered channel for user interrupts
    // ...
}

func (a *Agent) Run(ctx context.Context) {
    // Input goroutine — always reading
    safeGo(func() {
        scanner := bufio.NewScanner(os.Stdin)
        for scanner.Scan() {
            select {
            case a.interrupt <- scanner.Text():
            case <-ctx.Done():
                return
            }
        }
    })

    for {
        // Check for interrupt between iterations
        select {
        case msg := <-a.interrupt:
            a.history = append(a.history, Message{Role: "user", Content: msg})
        default:
            // no interrupt, prompt for input normally
            msg := a.tui.ReadInput()
            a.history = append(a.history, Message{Role: "user", Content: msg})
        }

        a.runAgentLoop(ctx)
    }
}
```

## Safety

- Tool safety levels: `Safe`, `NeedsConfirmation`, `Dangerous`
- Destructive shell commands require explicit user confirmation (colored red)
- File writes show colored diffs before applying
- No secrets logged or echoed in responses
- Sandboxed execution scope (current project directory)

## Project Structure

Domain-based layout following Go conventions. Start flat, grow when needed.

```
forge/
├── cmd/
│   └── forge/
│       └── main.go          ← thin: parse args, wire, run
├── internal/
│   ├── agent/               ← core domain
│   │   ├── agent.go         ← agent loop, prompt building
│   │   ├── executor.go      ← concurrent tool execution
│   │   └── context.go       ← conversation history, context window
│   ├── provider/            ← LLM provider domain
│   │   ├── provider.go      ← interface + factory
│   │   ├── ollama.go
│   │   ├── openai.go        ← also covers DeepSeek
│   │   ├── anthropic.go
│   │   ├── bedrock.go
│   │   └── alibaba.go
│   ├── tool/                ← tool domain
│   │   ├── tool.go          ← interface + registry
│   │   ├── file.go          ← read_file, write_file, list_directory
│   │   ├── shell.go         ← shell_exec
│   │   └── search.go        ← search_code
│   ├── config/              ← config loading
│   │   └── config.go
│   └── tui/                 ← terminal UI domain
│       ├── tui.go           ← main render loop
│       ├── color.go         ← ANSI color helpers
│       ├── spinner.go       ← animated spinners
│       ├── prompt.go        ← input prompt
│       └── markdown.go      ← markdown rendering
├── docs/
│   └── specs.md
├── go.mod
├── Makefile
└── README.md
```

`main.go` stays thin — parse command, wire dependencies, run:

```go
func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    cfg := config.Load()
    setupLogger(cfg.LogLevel, cfg.LogFormat)

    cmd := ""
    if len(os.Args) > 1 {
        cmd = os.Args[1]
    }

    switch cmd {
    case "chat":
        runChat(ctx, cfg)
    case "ask":
        runAsk(ctx, cfg, os.Args[2:])
    case "models":
        runModels(ctx, cfg)
    case "config":
        runConfig(cfg, os.Args[2:])
    case "update":
        runUpdate(ctx)
    case "version":
        fmt.Println("forge", version)
    default:
        runChat(ctx, cfg) // default to interactive chat
    }
}

func runChat(ctx context.Context, cfg *config.Config) {
    p := provider.New(cfg)
    tools := tool.Registry(cfg)
    ui := tui.New(cfg.Theme)
    a := agent.New(p, tools, ui)
    a.Run(ctx)
}
```

## Installation

### macOS

```bash
# Homebrew
brew tap forge-cli/tap
brew install forge

# Or download binary directly
curl -fsSL https://github.com/forge-cli/forge/releases/latest/download/forge-darwin-arm64 -o /usr/local/bin/forge
chmod +x /usr/local/bin/forge
```

### Windows

```powershell
# Scoop
scoop bucket add forge https://github.com/forge-cli/scoop-bucket
scoop install forge

# Or winget (once published)
winget install forge-cli.forge

# Or download binary directly
Invoke-WebRequest -Uri https://github.com/forge-cli/forge/releases/latest/download/forge-windows-amd64.exe -OutFile "$env:LOCALAPPDATA\forge\forge.exe"
# Add to PATH manually or via installer
```

### Install script (both platforms)

A one-liner that detects OS/arch and downloads the right binary:

```bash
curl -fsSL https://forge-cli.dev/install.sh | sh
```

```powershell
irm https://forge-cli.dev/install.ps1 | iex
```

### Release matrix

Cross-compiled via `GOOS`/`GOARCH` in CI (GitHub Actions):

| Platform       | Binary                       |
|----------------|------------------------------|
| macOS arm64    | `forge-darwin-arm64`         |
| macOS amd64    | `forge-darwin-amd64`         |
| Windows amd64  | `forge-windows-amd64.exe`    |
| Windows arm64  | `forge-windows-arm64.exe`    |
| Linux amd64    | `forge-linux-amd64`          |
| Linux arm64    | `forge-linux-arm64`          |

Go's cross-compilation makes this trivial — no CGO, single static binary per target.

### Auto-Update Check

On startup, Forge checks for new versions in a background goroutine (non-blocking):

1. Fetch `https://api.github.com/repos/forge-cli/forge/releases/latest` via `net/http`
2. Compare remote tag against embedded build version (`go build -ldflags "-X main.version=..."`)
3. If newer version exists, show a subtle notice after the session starts:

```
  ⚡ forge • ollama/llama3
  ℹ  Update available: v1.3.0 → v1.4.0  Run: forge update

  ❯ _
```

Behavior:
- Check runs at most once per 24 hours (last check timestamp stored in `~/.forge/state.json`)
- Never blocks startup — runs in background goroutine, result shown when ready
- `forge update` downloads the latest binary and replaces itself in-place
- `forge config set auto_update false` disables the check entirely
- Respects `FORGE_NO_UPDATE_CHECK=1` env var for CI environments

## Build & Run

```bash
# Build
go build -o forge ./cmd/forge

# Build with version info
go build -ldflags "-X main.version=v1.0.0" -o forge ./cmd/forge

# Test (always with race detector)
go test -race -v ./...

# Test with coverage
go test -race -cover -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run with local Ollama
forge chat

# Run with specific provider and model
forge chat --provider openai --model gpt-4o

# List available models
forge models

# Single-shot query
forge ask "explain this error" --file main.go

# Override via environment
FORGE_PROVIDER=deepseek FORGE_MODEL=deepseek-r1 forge chat
```

## Makefile

```makefile
.PHONY: build test lint clean install

VERSION ?= $(shell git describe --tags --always --dirty)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/forge ./cmd/forge

test:
	go test -race -v ./...

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	go vet ./...

clean:
	rm -rf bin/ coverage.out coverage.html

install: build
	cp bin/forge /usr/local/bin/forge

cross:
	GOOS=darwin  GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o bin/forge-darwin-arm64  ./cmd/forge
	GOOS=darwin  GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o bin/forge-darwin-amd64  ./cmd/forge
	GOOS=linux   GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o bin/forge-linux-amd64   ./cmd/forge
	GOOS=linux   GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o bin/forge-linux-arm64   ./cmd/forge
	GOOS=windows GOARCH=amd64 go build -ldflags "-X main.version=$(VERSION)" -o bin/forge-windows-amd64.exe ./cmd/forge
	GOOS=windows GOARCH=arm64 go build -ldflags "-X main.version=$(VERSION)" -o bin/forge-windows-arm64.exe ./cmd/forge
```

## Graceful Shutdown

Ctrl+C must not corrupt state. Forge uses `signal.NotifyContext` as the root context:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

Shutdown sequence:
1. Signal received → root context cancels
2. Cancel in-flight LLM API requests (context propagation)
3. Wait for running tool executions to finish (with 5s timeout)
4. Flush buffered output to terminal
5. Save current session to `~/.forge/sessions/`
6. Close provider connections
7. Exit cleanly

**Second signal = force exit:**

```go
safeGo(func() {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
    <-sigCh // second signal
    os.Exit(1)
})
```

All child operations (tool execution, streaming, indexing) derive contexts from the root. Cancellation propagates automatically.

## Error Handling

Errors are surfaced clearly in the terminal, never swallowed silently.

| Scenario | Behavior |
|---|---|
| Provider unreachable | Red error: `✗ Cannot reach ollama at localhost:11434`. Suggest checking if service is running. |
| Auth failure (bad API key) | Red error: `✗ Authentication failed for openai`. Don't echo the key. |
| Model not found | Red error: `✗ Model "gpt-5" not found`. Run `forge models` to list available. |
| Tool execution fails | Red inline: `✗ shell_exec failed: exit code 1` with stderr. Agent sees the error and can retry or explain. |
| Malformed model response | Log warning, skip the bad chunk, continue streaming. If unrecoverable, show `✗ Unexpected response from model` and let user retry. |
| Context window exceeded | Auto-compact: summarize older messages, drop tool results. Warn: `⚠ Context compacted — older messages summarized`. |
| Network timeout | Per-request timeout via `context.WithTimeout`. Streaming: 120s. Tool shell_exec: 30s. Show `✗ Request timed out`. Retry once for provider calls, then surface to user. |
| Config file missing | First-run wizard: prompt user to select provider and model. Write default config. |
| Config file malformed | Red error with line/position: `✗ config.json: invalid syntax at position 142`. |

All errors use `fmt.Errorf` with `%w` wrapping for context. The TUI layer formats them with color and icons.

**Sentinel errors** for domain conditions — check with `errors.Is`:

```go
var (
    ErrProviderUnreachable = errors.New("provider unreachable")
    ErrAuthFailed          = errors.New("authentication failed")
    ErrModelNotFound       = errors.New("model not found")
    ErrContextTooLong      = errors.New("context exceeds model limit")
    ErrAPIRateLimit        = errors.New("rate limit exceeded")
    ErrToolNotFound        = errors.New("tool not found")
    ErrToolTimeout         = errors.New("tool execution timed out")
)
```

Wrap with context at call sites: `fmt.Errorf("openai chat: %w", ErrAuthFailed)`. The agent loop checks `errors.Is(err, ErrAPIRateLimit)` to decide whether to retry or surface to the user.

## Testing Strategy

All tests use Go's standard `testing` package. No testify or external assertion libraries.

**Table-driven tests** for deterministic logic:

```go
func TestParseCommand(t *testing.T) {
    tests := []struct {
        name string
        args []string
        want string
    }{
        {"chat default", []string{"forge"}, "chat"},
        {"explicit chat", []string{"forge", "chat"}, "chat"},
        {"ask command", []string{"forge", "ask", "hello"}, "ask"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := parseCommand(tt.args)
            if got != tt.want {
                t.Errorf("parseCommand(%v) = %q, want %q", tt.args, got, tt.want)
            }
        })
    }
}
```

**Mock providers** via interface — test the agent loop without real API calls:

```go
type mockProvider struct {
    events []ChatEvent
}

func (m *mockProvider) ChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error) {
    ch := make(chan ChatEvent, len(m.events))
    for _, e := range m.events {
        ch <- e
    }
    close(ch)
    return ch, nil
}
```

**`httptest`** for provider HTTP integration tests — no real network, no flakiness.

**Test categories:**
- `internal/config/` — config loading, env var expansion, defaults
- `internal/provider/` — request building, response parsing, error mapping (via httptest)
- `internal/tool/` — tool execution, safety level checks
- `internal/agent/` — agent loop with mock provider and mock tools
- `internal/tui/` — color output, markdown rendering

**CI requirements:**
- `go test -race ./...` — always, no exceptions
- `go vet ./...` — static analysis
- Coverage target: aim for 80%+ on `internal/agent/` and `internal/provider/`

## Non-Goals (v1)

- GUI or web interface
- Multi-user / server mode
- Plugin marketplace
- Image/multimodal input
