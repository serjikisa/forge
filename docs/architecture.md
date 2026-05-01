# Architecture

Quick reference for Forge's component layout and data flow.

## Layer Diagram

```
  +--------------------------------------------------+
  |              Terminal UI Layer                    |
  |  [Prompt Engine] [Spinner/Status] [Syntax/MD]    |
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
- Reads user input (prompt, multi-line)
- Renders streaming tokens with ANSI colors
- Shows tool execution status (spinners, checkmarks)
- Handles terminal width detection (cross-platform)

**Agent (`internal/agent/`)**
- Owns the conversation loop (message → LLM → tools → repeat)
- Builds prompts with system instructions and tool definitions
- Manages context window (tracking token usage, compaction)
- Coordinates concurrent tool execution via bounded executor

**Provider (`internal/provider/`)**
- Implements the `Provider` interface per LLM service
- Handles auth, request building, streaming response parsing
- Manages HTTP client with timeouts, retry, rate limiting
- DeepSeek reuses OpenAI provider with different base URL

**Tool (`internal/tool/`)**
- Implements the `Tool` interface per capability
- Each tool declares its safety level (Safe / NeedsConfirmation / Dangerous)
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
