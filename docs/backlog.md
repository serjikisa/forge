# Backlog

## Open

- [ ] **LLM eval suite** — Add a dataset of prompts and expected agent behaviors (e.g. "read main.go" should call `read_file` with path `main.go`). Score how often each model picks the right tool with the right arguments. Track scores across models and prompt changes to catch regressions.
- [ ] **Homebrew distribution** — Create a Homebrew tap (forge-cli/tap) with a formula that builds from source or downloads a pre-built binary. Set up GitHub Actions to publish releases with goreleaser.
- [ ] **Versioning and update check** — Embed version at build time via ldflags. Add `forge update` or a startup check that compares local version against latest GitHub release and notifies the user.

## Done

- [x] list_directory boundary check
- [x] Malformed tool call handling (graceful recovery for empty/invalid JSON)
- [x] deepseek-r1 tool calling (auto-detect and disable tools immediately)
- [x] Strip echoed tool calls from text
- [x] Concurrent request safety (mutex on shared provider)
- [x] shell_exec boundary improvements (pipes, chains, subshells)
- [x] Streaming API (SSE endpoint /v1/chat/stream)
- [x] Token usage tracking (prompt + output displayed after each response)
- [x] Context window management (auto-compaction when history exceeds budget)
- [x] Model-specific prompt tuning (model_prompts in config)
- [x] Integration test cleanup (git checkout trap on exit)
- [x] Ctrl+J for multiline input
- [x] Markdown **bold** rendered as ANSI bold in terminal
- [x] Terminal width detection (actual terminal size)
- [x] Structured logging with slogr (pretty/text/JSON modes)
- [x] Conversation history for server mode (multi-role messages API)
- [x] Kiro-style TUI output (● bullets, action verbs, detail lines)
- [x] Orange FORGE braille logo
- [x] REST API server mode (`forge serve`)
- [x] Per-request model switching via API
- [x] Auto-disable tools for non-tool-calling models
- [x] Tool selection hints in system prompt
- [x] shell_exec project boundary check
- [x] write_file go.mod subdirectory protection
- [x] Integration test script with auto-discovery
- [x] `--yes` flag for auto-approve
- [x] Ctrl+C graceful shutdown for serve mode
