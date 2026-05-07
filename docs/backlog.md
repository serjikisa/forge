# Backlog

## High Priority

- [ ] **list_directory boundary check** — `list_directory` allows listing directories outside the project (e.g. `/etc`). Should enforce the same `inProject()` check as `read_file`/`write_file`.
- [ ] **Malformed tool call handling** — llama3.2 generates empty or malformed JSON arguments (`unexpected end of JSON input`). Add graceful error recovery instead of passing errors back to the model in a loop.
- [ ] **deepseek-r1 tool calling** — Model accepts tools silently but never calls them. Auto-disable kicks in after 2 strikes, but ideally detect this from model metadata or first response and skip tools immediately.

## Medium Priority

(empty)

## Low Priority

- [ ] **LLM eval suite** — Add a dataset of prompts and expected agent behaviors (e.g. "read main.go" should call `read_file` with path `main.go`). Score how often each model picks the right tool with the right arguments. Track scores across models and prompt changes to catch regressions.

## Done

- [x] Ctrl+J for multiline input
- [x] Markdown **bold** rendered as ANSI bold in terminal
- [x] Terminal width detection (was hardcoded 80, now uses actual size)
- [x] Structured logging with slogr (pretty/text/JSON modes)
- [x] Conversation history for server mode (multi-role messages API)
- [x] Strip echoed tool calls from text
- [x] Concurrent request safety (mutex on shared provider)
- [x] shell_exec boundary improvements (pipes, chains, subshells)
- [x] Streaming API (SSE endpoint /v1/chat/stream)
- [x] Token usage tracking (prompt + output displayed after each response)
- [x] Context window management (auto-compaction when history exceeds budget)
- [x] Model-specific prompt tuning (model_prompts in config)
- [x] Integration test cleanup (git checkout trap on exit)
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
