# Backlog

## High Priority

- [ ] **list_directory boundary check** — `list_directory` allows listing directories outside the project (e.g. `/etc`). Should enforce the same `inProject()` check as `read_file`/`write_file`.
- [ ] **Malformed tool call handling** — llama3.2 generates empty or malformed JSON arguments (`unexpected end of JSON input`). Add graceful error recovery instead of passing errors back to the model in a loop.
- [ ] **deepseek-r1 tool calling** — Model accepts tools silently but never calls them. Auto-disable kicks in after 2 strikes, but ideally detect this from model metadata or first response and skip tools immediately.

## Medium Priority

- [ ] **Strip echoed tool calls from text** — qwen2.5-coder echoes the JSON tool call in its text response. The texttools parser extracts the call but leaves the JSON in the displayed text. Should strip it cleanly.
- [ ] **Concurrent request safety** — Server switches model on the shared provider with `SetModel()`. Concurrent requests with different models will race. Use per-request provider instances or a mutex.
- [ ] **shell_exec boundary improvements** — Current `extractTargetPath` only catches simple patterns (`cat /etc/passwd`). Piped commands (`cat /etc/passwd | grep root`) and subshells bypass it.

## Low Priority

- [ ] **LLM eval suite** — Add a dataset of prompts and expected agent behaviors (e.g. "read main.go" should call `read_file` with path `main.go`). Score how often each model picks the right tool with the right arguments. Track scores across models and prompt changes to catch regressions.
- [ ] **Integration test cleanup** — Models can create files during tests (e.g. llama3.2:3b created `internal/go.mod`). Add a post-test `git checkout -- .` or run tests in a temp copy.
- [ ] **Model-specific prompt tuning** — Different models respond better to different prompt styles. Add per-model prompt overrides in config.
- [ ] **Streaming API** — Add SSE/WebSocket endpoint for streaming responses instead of waiting for full completion.
- [ ] **Token usage tracking** — Track and display token counts per request for cost awareness.
- [ ] **Context window management** — Implement conversation compaction when history exceeds model context limit (described in prompts.md but not implemented).

## Done

- [x] Ctrl+J for multiline input
- [x] Markdown **bold** rendered as ANSI bold in terminal
- [x] Terminal width detection (was hardcoded 80, now uses actual size)
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
