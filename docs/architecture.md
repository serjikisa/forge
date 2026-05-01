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
  |  Ollama | OpenAI | Anthropic | Bedrock | DeepSeek |
  +--------------------------------------------------+
  |              Context & State                      |
  |  History | Config | Project Boundary | Sessions   |
  +--------------------------------------------------+
```

## Data Flow

```
  User Input
      |
      v
  [TUI: Prompt] --> parse input / slash commands
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
- Defines `UI` interface implemented by `TUI` (terminal), `HeadlessTUI` (server), and `StreamingTUI` (SSE)
- Raw-mode terminal input via `golang.org/x/term` with Ctrl-C/Ctrl-J handling
- Renders Kiro-style tool annotations (colored bullets, action verbs, detail lines)
- Animated spinner during LLM thinking
- Inline **bold** markdown rendering

**Server (`internal/server/`)**
- HTTP server exposing `POST /v1/chat`, `POST /v1/chat/stream`, and `GET /health`
- Creates a fresh agent per request with `HeadlessTUI` or `StreamingTUI`
- Supports per-request model switching via `model` field
- Auto-approves all tool calls in server mode
- Mutex-protected to prevent concurrent model switching

**Agent (`internal/agent/`)**
- Owns the conversation loop (message -> LLM -> tools -> repeat)
- Builds prompts with system instructions and tool definitions
- Parses text-based tool calls for models without native tool calling
- Auto-disables tools for models that ignore them (2-strike rule)
- History compaction when context budget exceeded
- Session save/load for conversation persistence
- Slash commands: /help, /clear, /save, /resume, /model

**Provider (`internal/provider/`)**
- Implements the `Provider` interface per LLM service
- Ollama: NDJSON streaming, auto model detection, parameter size query
- OpenAI: SSE streaming with tool call fragment accumulation
- Anthropic: SSE streaming with content block events
- Bedrock: AWS Event Stream binary protocol with SigV4 signing (no SDK)
- DeepSeek: reuses OpenAI provider with different base URL

**Tool (`internal/tool/`)**
- Implements the `Tool` interface per capability
- 8 tools: read_file, write_file, edit_file, list_directory, shell_exec, search_code, web_search, web_fetch
- Each tool declares its safety level (Safe / NeedsConfirmation)
- Project boundary enforcement via `.git` root detection
- Shell timeout configurable via config

**Config (`internal/config/`)**
- Loads `~/.forge/config.json` at startup
- Expands env vars via `os.ExpandEnv`
- Graceful fallback to defaults on malformed config
- Creates default config on first run

## Concurrency Model

```
  main goroutine
      |
      +-- [TUI input loop] (raw mode via x/term)
      |
      +-- [Agent loop]
      |       |
      |       +-- [Provider streaming goroutine] (reads HTTP response body)
      |       |
      |       +-- [Tool executor goroutines] (bounded by semaphore, max 5)
      |       |       +-- tool 1
      |       |       +-- tool 2
      |       |       +-- ...
      |       |
      |       +-- [Spinner animation goroutine]
      |
      +-- [Background: Ollama parameter size fetch]
```

Tool executor goroutines:
- Bounded by semaphore (`maxConcurrency` from config)
- Panic recovery per goroutine
- Results collected in order via indexed slice (no mutex needed)
- Context cancellation checked in tools

## Local Storage

```
~/.forge/
├── config.json      -- user configuration
└── sessions/        -- persisted conversations (via /save)
    └── 2026-05-01_14-30-05.json
```
