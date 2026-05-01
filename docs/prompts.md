# Prompts

The system prompt is the soul of Forge. It defines how the agent behaves, when it uses tools, and how it communicates with the user.

## System Prompt Structure

The system prompt is assembled dynamically per request:

```
[Identity]        -- who Forge is
[Capabilities]    -- what tools are available
[Tool Definitions]-- JSON schema for each tool (auto-generated)
[Rules]           -- behavioral guidelines
[Context]         -- current working directory, OS, project info
```

## Identity Block

```
You are Forge, a terminal-based AI coding assistant. You help developers
write, debug, and understand code directly from the command line.

You are direct and concise. You write complete, working code. You explain
your reasoning when making decisions. You ask for clarification only when
the request is genuinely ambiguous.
```

## Capabilities Block

```
You have access to tools to interact with the user's system:

- read_file: Read the contents of a file
- write_file: Create or overwrite files
- edit_file: Replace a specific string in a file (targeted edits)
- list_directory: List files and directories
- shell_exec: Execute a shell command
- search_code: Search for patterns across the codebase
- web_search: Search the web using DuckDuckGo
- web_fetch: Fetch and read a web page

Use tools when you need to inspect or modify the user's project.
Read files before making claims about their contents.
```

The tool list is generated from the registered tools at startup.

## Rules Block

```
Rules:
- Read code before editing it. Never guess at file contents.
- Show what you changed and why.
- For destructive operations (deleting files, overwriting), explain
  what will happen and confirm before proceeding.
- Keep responses concise. Code speaks louder than explanations.
- If a task requires multiple steps, outline your plan first.
- When you encounter an error, diagnose the root cause rather than
  making incremental patches.
- Do not fabricate file contents, command outputs, or tool results.
- If you don't know something, say so.
```

## Context Block (injected at runtime)

```
Current directory: /Users/dev/myproject
Operating system: darwin/arm64
Shell: /bin/zsh
Project type: Go module (go.mod found)
```

This is populated by inspecting the user's environment at startup. Helps the model give OS-appropriate commands and understand the project.

## Tool Definitions

Auto-generated from the `Tool` interface. Each tool's `Schema()` method returns its JSON Schema, which is injected into the prompt in the provider's expected format.

Example (OpenAI format):
```json
{
  "type": "function",
  "function": {
    "name": "read_file",
    "description": "Read the contents of a file at the given path",
    "parameters": {
      "type": "object",
      "properties": {
        "path": {
          "type": "string",
          "description": "Absolute or relative file path"
        }
      },
      "required": ["path"]
    }
  }
}
```

The provider layer translates this to each API's expected format (see providers.md).

## Conversation History Format

Messages are stored as a flat list:

```go
type Message struct {
    Role       string          // "system", "user", "assistant", "tool"
    Content    string          // text content
    ToolCalls  []ToolCall      // assistant requesting tool use
    ToolCallID string          // for tool result messages
}
```

History is sent to the LLM on every request. When the context window fills up, older messages are compacted (summarized or dropped).

## Context Window Management

Each model has a token limit. Forge tracks approximate token usage:

1. System prompt + tool definitions = fixed overhead (measured once)
2. Conversation history = grows with each turn
3. When usage exceeds 80% of model limit:
   - Summarize older messages into a single "conversation so far" block
   - Drop tool result contents (keep tool call names for continuity)
   - Keep the most recent N turns intact

Token estimation: ~3 characters per token (conservative for code-heavy content). Exact counts come from provider usage data when available.

## Prompt Tuning Per Model

Different models respond differently to the same prompt. Known adjustments:

**Ollama / local models (Llama, Qwen, Mistral):**
- Simpler, more explicit instructions
- Fewer tools at once (small context windows)
- May need examples of tool call format in the system prompt
- Some models need explicit "you MUST use tools" nudging

**OpenAI (GPT-4o):**
- Follows instructions well, minimal nudging needed
- Handles many tools simultaneously
- Good at multi-step reasoning

**Anthropic (Claude):**
- Excellent at following detailed instructions
- Tends to be verbose — add "be concise" to system prompt
- Strong tool calling reliability

**DeepSeek:**
- R1 models may include `<think>` blocks — decide whether to show or strip
- Tool calling less reliable than OpenAI/Anthropic on complex chains

## Single-Shot vs Chat Mode

**Chat mode (`forge chat`):**
- Full conversation history maintained
- System prompt sent once, history grows
- Context compaction when needed

**Single-shot mode (`forge ask "..."`):**
- No history — just system prompt + user message
- Tool calls still work (agent loop runs until text response)
- Output goes to stdout for piping: `forge ask "explain main.go" | less`

## Prompt Testing

Prompts are the hardest part to get right. Test them by:

1. Create test fixtures with known inputs and expected tool call sequences
2. Run against mock providers to verify the agent makes the right tool calls
3. Log full prompt + response pairs during development (`--log-level debug`)
4. Iterate on the system prompt based on real usage patterns

Prompt changes should be treated like code changes — reviewed, tested, and versioned.
