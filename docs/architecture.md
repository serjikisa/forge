# Architecture

Quick reference for Forge's component layout and data flow.

## Layer Diagram

```
  +--------------------------------------------------+
  |              Interface Layer                      |
  |  [Terminal TUI]  [REST API Server]  [Headless]   |
  +--------------------------------------------------+
  |              Agent Orchestrator                   |
  |  [Tool Executor] [Text Tool Parser] [Safety]     |
  +--------------------------------------------------+
  |              Provider Layer                       |
  |  Ollama | OpenAI | Anthropic | Bedrock | ...      |
  +--------------------------------------------------+
  |              Context & State                      |
  |  History | Config | Project Boundary              |
  +--------------------------------------------------+
```

## Data Flow

```
  User Input
      |
      v
  [TUI: Prompt] --> parse input
      |
      v
  [Agent] --> build messages (system prompt + history + tools)
      |
      v
  [Provider] --> HTTP POST to LLM API (streaming)
      |
      v
  [Agent] <-- stream ChatEvents via channel
      |
      +-- text delta --> [TUI: render tokens]
      |
      +-- tool_call --> [Executor: run tools concurrently]
                              |
                              v
                        [Tool results]
                              |
                              v
                        [Agent] --> append results, loop back to Provider
```

## Component Responsibilities

**Terminal UI (`internal/tui/`)**
- Defines `UI` interface implemented by `TUI` (terminal) and `HeadlessTUI` (server)
- Reads user input (prompt, multi-line)
- Renders Kiro-style tool annotations (● bullets, action verbs, detail lines)
- Shows tool execution status (spinners, checkmarks)
- `HeadlessTUI` captures output as structured `Event` objects for the REST API

**Server (`internal/server/`)**
- HTTP server exposing `POST /v1/chat` and `GET /health`
- Creates a fresh agent per request with `HeadlessTUI`
- Supports per-request model switching via `model` field
- Auto-approves all tool calls in server mode

**Agent (`internal/agent/`)**
- Owns the conversation loop (message → LLM → tools → repeat)
- Builds prompts with system instructions and tool definitions
- Parses text-based tool calls for models without native tool calling
- Auto-disables tools for models that don't use them
- Supports auto-approve mode (`--yes`) to skip confirmation prompts

**Provider (`internal/provider/`)**
- Implements the `Provider` interface per LLM service
- Handles auth, request building, streaming response parsing
- Manages HTTP client with timeouts, retry, rate limiting
- DeepSeek reuses OpenAI provider with different base URL

**Tool (`internal/tool/`)**
- Implements the `Tool` interface per capability
- Each tool declares its safety level (Safe / NeedsConfirmation / Dangerous)
- Project boundary enforcement: `read_file`, `write_file`, and `shell_exec` block access outside project root
- `shell_exec` detects file-accessing commands (cat, head, cp, etc.) targeting paths outside the project
- `write_file` prevents creating `go.mod`/`go.sum` in subdirectories
- Tools accept `context.Context` for cancellation support

**Config (`internal/config/`)**
- Loads `~/.forge/config.json` at startup
- Expands env vars via `os.ExpandEnv`
- Provides `mustGetEnv` / `getEnv` helpers
- Manages `~/.forge/state.json` for runtime state

## Concurrency Model

```
  main goroutine
      |
      +-- [TUI input loop]
      |
      +-- [Agent loop]
      |       |
      |       +-- [Provider streaming goroutine]
      |       |
      |       +-- [Tool executor goroutines] (bounded by semaphore)
      |       |       +-- tool 1
      |       |       +-- tool 2
      |       |       +-- ...
      |       |
      |       +-- [Spinner animation goroutine]
      |
      +-- [Background: session auto-save]
      +-- [Background: file indexing]
      +-- [Background: update check]
```

All goroutines:
- Launch via `safeGo` (panic recovery)
- Check `ctx.Done()` in every `select`
- Derive context from root `signal.NotifyContext`

## Local Storage

```
~/.forge/
├── config.json      -- user configuration
├── state.json       -- runtime state (update check, last session)
└── sessions/        -- persisted conversations
    └── *.json
```
