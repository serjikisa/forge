# Implementation Tasks — v0.1 (Ollama-Only)

## Scope

v0.1 is a working CLI agent that connects to a local Ollama instance. No cloud providers. The goal is to get the core agent loop working end-to-end: user types a message, Forge sends it to Ollama, streams the response, handles tool calls, and displays the result.

## What's IN v0.1

- `forge chat` — interactive conversation with Ollama
- `forge ask` — single-shot query (with `--file` flag)
- `forge models` — list locally installed Ollama models
- `forge version` — print version
- Ollama provider (streaming, tool calling)
- Core tools: `read_file`, `write_file`, `list_directory`, `shell_exec`, `search_code`
- Permission system: `ask` / `allow` / `deny` per category (file_read, file_write, shell, web)
- User interrupt: send prompts while agent is working
- Basic TUI: colored prompt, streaming output, tool status, permission prompts
- Config loading from `~/.forge/config.json`
- Graceful shutdown (Ctrl+C)
- Structured logging (slog)

## What's OUT of v0.1

- Cloud providers (OpenAI, Anthropic, Bedrock, DeepSeek, Alibaba)
- `web_search` tool (permission system ready, but search disabled until provider added)
- External tools (config schema defined, implementation deferred)
- `forge update` / auto-update check
- `forge config` interactive wizard
- Session persistence / resume
- Context window compaction
- Themes (just use `vibrant` hardcoded)
- Markdown rendering (plain text output is fine)
- Syntax highlighting
- Background file indexing

---

## Phase 1: Project Skeleton

### Task 1.1: Initialize Go module and project structure
```
forge/
├── cmd/forge/main.go
├── internal/
│   ├── agent/
│   ├── provider/
│   ├── tool/
│   ├── config/
│   └── tui/
├── go.mod
└── Makefile
```
- `go mod init github.com/forge-cli/forge`
- Create empty packages with placeholder files
- Makefile with `build`, `test`, `lint`, `clean` targets
- Verify `make build` produces a binary

**Done when:** `./bin/forge version` prints `forge dev`

### Task 1.2: Config loading
- `internal/config/config.go`
- `Config` struct with fields: `DefaultProvider`, `LogLevel`, `LogFormat`, `MaxConcurrency`, `Providers` map
- `Load()` reads `~/.forge/config.json`, falls back to defaults
- `os.ExpandEnv` on raw JSON before unmarshaling
- `mustGetEnv` / `getEnv` helpers
- Create `~/.forge/` directory if missing

**Done when:** `config.Load()` returns a valid config with Ollama defaults when no config file exists

### Task 1.3: Structured logging setup
- `setupLogger(level, format string)` in `main.go`
- Text handler to stderr
- Configurable via `--log-level` flag and `FORGE_LOG_LEVEL` env var

**Done when:** `forge chat --log-level debug` shows debug logs on stderr

---

## Phase 2: Ollama Provider

### Task 2.1: Provider interface and types
- `internal/provider/provider.go`
- Define `Provider` interface, `ChatRequest`, `ChatEvent`, `EventType`, `Message`, `ToolCall`, `ToolResult`, `Model`
- `New(cfg)` factory function that returns the configured provider

**Done when:** Types compile, factory returns an Ollama provider

### Task 2.2: Ollama — ListModels
- `internal/provider/ollama.go`
- `GET /api/tags` → parse response → return `[]Model`
- HTTP client with timeout, context support

**Done when:** `forge models` prints locally installed Ollama models

### Task 2.3: Ollama — ChatCompletion (text only, no tools)
- `POST /api/chat` with `stream: true`
- Parse NDJSON stream line by line
- Send `ChatEvent` (type=EventText) per chunk to channel
- Send `EventDone` when `"done": true`
- Context cancellation support

**Done when:** `forge chat` streams a text response from Ollama token by token

### Task 2.4: Ollama — Tool calling support
- Include `tools` array in request when tools are provided
- Parse `message.tool_calls` from response
- Send `ChatEvent` (type=EventToolCall) with parsed `ToolCall`
- Handle the case where Ollama returns text + tool calls in same response

**Done when:** Ollama responds with tool calls that are correctly parsed into `ToolCall` structs

---

## Phase 3: Tool System

### Task 3.1: Tool interface and registry
- `internal/tool/tool.go`
- `Tool` interface, `SafetyLevel` enum
- `Registry(cfg)` returns list of built-in tools

**Done when:** `Registry(cfg)` returns 5 tools with correct names and schemas

### Task 3.2: read_file tool
- `internal/tool/file.go`
- Read file contents, respect project directory boundary
- Binary file detection (null bytes in first 512 bytes)
- `offset` and `limit` parameters
- Symlink resolution + boundary check

**Done when:** Tool reads files, rejects paths outside project dir, detects binary files

### Task 3.3: write_file tool
- Same file, `WriteFile` struct
- Create parent dirs with `os.MkdirAll`
- Safety level: `NeedsConfirmation`
- Project directory boundary check

**Done when:** Tool creates/overwrites files with confirmation prompt (confirmation handled by agent, not tool)

### Task 3.4: list_directory tool
- Same file, `ListDirectory` struct
- `filepath.WalkDir` with depth limit
- Skip `.git/`, `node_modules/`, common ignore patterns
- Cap at 200 entries

**Done when:** Tool lists directory contents with depth control

### Task 3.5: shell_exec tool
- `internal/tool/shell.go`
- `os/exec` with context timeout (30s default)
- Capture stdout + stderr
- Return exit code in result
- Safety level: `NeedsConfirmation`, elevated to `Dangerous` for destructive patterns

**Done when:** Tool executes commands, returns output, respects timeout, detects dangerous commands

### Task 3.6: search_code tool
- `internal/tool/search.go`
- `regexp` + `filepath.WalkDir`
- Skip binary files, `.git/`, `node_modules/`
- Return matches with file:line format
- `max_results` cap (default 50)

**Done when:** Tool searches codebase with regex, returns formatted results

---

## Phase 4: Agent Loop

### Task 4.1: Basic agent loop (text only)
- `internal/agent/agent.go`
- `Agent` struct with provider, tools, tui
- `Run(ctx)` — read input → build messages → call provider → stream response → repeat
- System prompt assembly (identity + capabilities + rules + context)
- Conversation history in memory

**Done when:** User can have a multi-turn text conversation with Ollama via Forge

### Task 4.2: Tool execution in agent loop
- When provider returns `EventToolCall`:
  - Look up tool by name
  - Check safety level, prompt for confirmation if needed
  - Execute tool
  - Append tool result as message
  - Call provider again with updated history
- Loop until provider returns text (no more tool calls)

**Done when:** Agent can read files, run commands, and search code when asked by the user

### Task 4.3: Concurrent tool executor
- `internal/agent/executor.go`
- `Executor` struct with `maxConcurrency`
- `RunTools(ctx, calls)` — bounded parallel execution with panic recovery
- Results preserve ordering

**Done when:** Multiple tool calls execute concurrently, panics are captured as errors

### Task 4.4: Permission system
- `internal/agent/permission.go`
- `Permission` type: `Ask`, `Allow`, `Deny`
- Permission categories: `file_read`, `file_write`, `shell`, `web`, `external`
- Load defaults from config, override with `--allow-*` flags
- `CheckPermission(category, description)` — returns allow/deny
  - If `Allow`: auto-approve
  - If `Ask`: prompt user via TUI, offer `[y]es / [n]o / [a]llow all for session`
  - If `Deny`: return error, agent sees tool was denied
- Session-level overrides (user picks "allow all" during prompt)

**Done when:** `write_file` and `shell_exec` prompt for permission, user can allow-all per category

### Task 4.5: User interrupt (send prompts while agent works)
- Dedicated input goroutine that reads stdin at all times
- Buffered interrupt channel on the `Agent` struct
- Agent checks interrupt channel between tool call batches
- On interrupt: cancel in-flight tools, append user message, restart agent loop
- TUI shows `⚠ Interrupted — redirecting...`

**Done when:** User can type a new message while agent is executing tools, agent redirects

---

## Phase 5: Terminal UI

### Task 5.1: Color helpers
- `internal/tui/color.go`
- ANSI escape code functions: `Cyan()`, `Green()`, `Yellow()`, `Red()`, `Magenta()`, `Dim()`, `Bold()`
- `NO_COLOR` env var support (disable colors)

**Done when:** Color functions produce correct ANSI output, respect `NO_COLOR`

### Task 5.2: Prompt and input
- `internal/tui/prompt.go`
- Branded prompt: `⚡ forge • ollama/llama3`
- `bufio.Scanner` for user input
- Multi-line support (backslash continuation)

**Done when:** User sees the branded prompt and can type multi-line input

### Task 5.3: Streaming output
- `internal/tui/tui.go`
- `TUI` struct with `RenderStream(events <-chan ChatEvent)`
- Print text deltas as they arrive (no buffering)
- Show tool call status: `◐ Reading main.go...` → `✓ read_file main.go`

**Done when:** Responses stream token by token, tool calls show status with spinners

### Task 5.4: Spinner animation
- `internal/tui/spinner.go`
- Background goroutine cycles through `◐ ◑ ◒ ◓`
- Start/stop methods
- Yellow while running, green on success, red on error

**Done when:** Spinners animate during tool execution

### Task 5.5: Confirmation prompts
- For `NeedsConfirmation` and `Dangerous` tools
- Show command/file path in yellow/red
- `[y/N]` prompt, default to No
- Dangerous commands shown in red with warning

**Done when:** `write_file` and `shell_exec` ask for confirmation before executing

---

## Phase 6: CLI Commands

### Task 6.1: Command routing in main.go
- Parse `os.Args` for subcommand
- Wire `forge chat`, `forge ask`, `forge models`, `forge version`
- `--provider`, `--model`, `--log-level` flags via `flag` package
- Signal context setup (`signal.NotifyContext`)
- Graceful shutdown: cancel context → wait for agent → exit

**Done when:** All 4 commands route correctly, Ctrl+C shuts down cleanly

### Task 6.2: `forge ask` with `--file` flag
- Single-shot mode: send one message, run agent loop until text response, exit
- `--file` flag: read file, append as code block to user message
- Output to stdout (no prompt, no spinner — clean for piping)

**Done when:** `forge ask "explain this" --file main.go` prints a response and exits

---

## Phase 7: Testing

### Task 7.1: Config tests
- Table-driven tests for `Load()` with various config files
- Env var expansion tests
- Missing config file → defaults
- Malformed JSON → error

### Task 7.2: Ollama provider tests
- `httptest.NewServer` mock for Ollama API
- Test request building (message format, tool inclusion)
- Test streaming response parsing (text, tool calls, errors)
- Test context cancellation mid-stream

### Task 7.3: Tool tests
- Table-driven tests per tool
- `t.TempDir()` for file operation tests
- Project directory boundary enforcement
- Binary file detection
- Shell timeout
- Search with various patterns

### Task 7.4: Agent loop tests
- Mock provider + mock tools
- Test text-only conversation
- Test tool call → result → response cycle
- Test multi-tool concurrent execution
- Test panic recovery in tool execution

### Task 7.5: TUI tests
- Color output with and without `NO_COLOR`
- Markdown-free output verification

**Done when:** `make test` passes with `-race`, 80%+ coverage on agent and provider packages

---

## Implementation Order

```
Phase 1 (skeleton)     ████░░░░░░  ~1 day
Phase 2 (ollama)       ████████░░  ~2 days
Phase 3 (tools)        ████████░░  ~2 days
Phase 4 (agent loop)   ██████████  ~3-4 days  (includes permissions + interrupt)
Phase 5 (TUI)          ██████░░░░  ~1-2 days
Phase 6 (CLI)          ████░░░░░░  ~1 day
Phase 7 (testing)      ██████████  ~2 days
                                   -----------
                                   ~12-15 days
```

Phases 2 and 3 can be worked in parallel. Phase 4 depends on both. Phase 5 can start once Phase 4.1 is done.

## Definition of Done — v0.1

- [ ] `forge chat` connects to Ollama and has a multi-turn conversation
- [ ] Agent uses tools (read files, run commands, search code) when appropriate
- [ ] Tool calls execute concurrently when multiple are requested
- [ ] Permission prompts for file writes, shell commands (with allow-all option)
- [ ] User can send a message while agent is working to redirect it
- [ ] Streaming responses appear token by token
- [ ] Ctrl+C shuts down cleanly
- [ ] `forge ask "question" --file main.go` works for single-shot queries
- [ ] `forge models` lists installed Ollama models
- [ ] `make test` passes with `-race` flag
- [ ] Single binary, zero dependencies, builds on macOS/Windows/Linux
