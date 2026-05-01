# Providers

Each LLM provider has different auth, message formats, streaming protocols, and tool calling conventions. This doc covers the implementation details per provider.

## Provider Interface

```go
type Provider interface {
    ChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
    ListModels(ctx context.Context) ([]Model, error)
    Name() string
    SupportsToolCalling() bool
}
```

## Ollama (Local)

| Field | Value |
|-------|-------|
| Base URL | `http://localhost:11434` (configurable) |
| Auth | None |
| Streaming | NDJSON (`application/x-ndjson`) |
| Tool calling | Supported (Llama 3.1+, Qwen 2.5+, Mistral) |
| Models endpoint | `GET /api/tags` |
| Chat endpoint | `POST /api/chat` |

**Message format:**
```json
{
  "model": "llama3",
  "messages": [
    {"role": "system", "content": "..."},
    {"role": "user", "content": "..."}
  ],
  "stream": true,
  "tools": [...]
}
```

**Streaming:** Each line is a JSON object with `"done": false` until the final chunk. Tool calls arrive in `message.tool_calls`.

**Tool call format (Ollama uses OpenAI-style):**
```json
{
  "message": {
    "role": "assistant",
    "content": "",
    "tool_calls": [
      {
        "function": {
          "name": "read_file",
          "arguments": {"path": "main.go"}
        }
      }
    ]
  },
  "done": false
}
```

Note: Ollama tool calls arrive as complete objects (not streamed fragments like OpenAI). The `arguments` field is a parsed object, not a JSON string.

**Tool result format:**
```json
{
  "role": "tool",
  "content": "package main\n\nimport \"fmt\"\n..."
}
```

Tool results are sent as messages with `role: "tool"` in the next request's messages array.

**Quirks:**
- Model must be pulled first (`ollama pull llama3`)
- Tool calling quality varies significantly by model
- No rate limits (local)
- Auto-discovery via `GET /api/tags` to list installed models

---

## OpenAI

| Field | Value |
|-------|-------|
| Base URL | `https://api.openai.com/v1` |
| Auth | `Authorization: Bearer $OPENAI_API_KEY` |
| Streaming | SSE (`text/event-stream`) |
| Tool calling | Full support |
| Models endpoint | `GET /v1/models` |
| Chat endpoint | `POST /v1/chat/completions` |

**Message format:**
```json
{
  "model": "gpt-4o",
  "messages": [
    {"role": "system", "content": "..."},
    {"role": "user", "content": "..."}
  ],
  "stream": true,
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "read_file",
        "description": "Read file contents",
        "parameters": { "type": "object", "properties": {...} }
      }
    }
  ]
}
```

**Streaming:** SSE with `data: {...}` lines. Each chunk has `choices[0].delta` with partial content or tool call fragments. Stream ends with `data: [DONE]`.

**Tool call response format:**
```json
{
  "choices": [{
    "delta": {
      "tool_calls": [{
        "index": 0,
        "id": "call_abc123",
        "function": { "name": "read_file", "arguments": "{\"path\":\"main.go\"}" }
      }]
    }
  }]
}
```

Tool call arguments arrive as streamed JSON fragments — accumulate and parse after stream ends.

**Rate limits:** Varies by tier. Watch for `429` responses. Use `Retry-After` header or exponential backoff.

---

## DeepSeek

| Field | Value |
|-------|-------|
| Base URL | `https://api.deepseek.com/v1` |
| Auth | `Authorization: Bearer $DEEPSEEK_API_KEY` |
| Streaming | SSE (OpenAI-compatible) |
| Tool calling | Supported (DeepSeek-V3) |
| Chat endpoint | `POST /v1/chat/completions` |

**Implementation:** Reuses the OpenAI provider with a different `base_url`. The API is OpenAI-compatible.

**Quirks:**
- DeepSeek-R1 (reasoning model) may not support tool calling reliably
- Rate limits are more aggressive than OpenAI
- `<think>` tags in responses for reasoning models — strip or display based on user preference

---

## Anthropic

| Field | Value |
|-------|-------|
| Base URL | `https://api.anthropic.com/v1` |
| Auth | `x-api-key: $ANTHROPIC_API_KEY` + `anthropic-version: 2023-06-01` |
| Streaming | SSE (`text/event-stream`) |
| Tool calling | Full support |
| Chat endpoint | `POST /v1/messages` |

**Message format (different from OpenAI):**
```json
{
  "model": "claude-sonnet-4-20250514",
  "max_tokens": 4096,
  "system": "You are a coding assistant...",
  "messages": [
    {"role": "user", "content": "..."}
  ],
  "stream": true,
  "tools": [
    {
      "name": "read_file",
      "description": "Read file contents",
      "input_schema": { "type": "object", "properties": {...} }
    }
  ]
}
```

Key differences from OpenAI:
- `system` is a top-level field, not a message
- Tool schema uses `input_schema` not `parameters`
- No `type: "function"` wrapper
- `max_tokens` is required

**Streaming events:**
```
event: content_block_start
event: content_block_delta    (text or tool input)
event: content_block_stop
event: message_delta
event: message_stop
```

**Tool call response:** Tool use arrives as a `content_block_start` with `type: "tool_use"`, followed by `content_block_delta` events with `input_json_delta`.

**Tool result format:**
```json
{
  "role": "user",
  "content": [
    {
      "type": "tool_result",
      "tool_use_id": "toolu_abc123",
      "content": "file contents here..."
    }
  ]
}
```

**Rate limits:** Per-model limits. Use `retry-after` header.

---

## AWS Bedrock

| Field | Value |
|-------|-------|
| Base URL | `https://bedrock-runtime.{region}.amazonaws.com` |
| Auth | AWS Signature V4 (via SDK or manual signing) |
| Streaming | Event stream (AWS-specific binary framing) |
| Tool calling | Supported (Converse API) |
| Chat endpoint | `POST /model/{modelId}/converse-stream` |

**Auth:** Uses AWS Signature V4. Without the AWS SDK, implement signing manually using `crypto/hmac` and `crypto/sha256` (stdlib). Requires `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and optionally `AWS_SESSION_TOKEN`.

**Message format (Converse API):**
```json
{
  "messages": [
    {
      "role": "user",
      "content": [{"text": "..."}]
    }
  ],
  "system": [{"text": "..."}],
  "toolConfig": {
    "tools": [
      {
        "toolSpec": {
          "name": "read_file",
          "description": "Read file contents",
          "inputSchema": {
            "json": { "type": "object", "properties": {...} }
          }
        }
      }
    ]
  }
}
```

Key differences:
- Content is an array of blocks, not a string
- Tool schema wrapped in `toolSpec` / `inputSchema` / `json`
- System prompt is an array of text blocks
- Streaming uses AWS event stream binary protocol, not SSE

**Quirks:**
- Signing requests without the AWS SDK is complex but doable with stdlib
- Region-specific endpoints
- Model IDs include version: `anthropic.claude-sonnet-4-20250514-v1:0`
- IAM permissions required: `bedrock:InvokeModelWithResponseStream`

---

## Alibaba Cloud (DashScope)

| Field | Value |
|-------|-------|
| Base URL | `https://dashscope.aliyuncs.com/api/v1` |
| Auth | `Authorization: Bearer $DASHSCOPE_API_KEY` |
| Streaming | SSE (`text/event-stream`) |
| Tool calling | Supported (Qwen-Max, Qwen-Plus) |
| Chat endpoint | `POST /services/aigc/text-generation/generation` |

**Message format:**
```json
{
  "model": "qwen-max",
  "input": {
    "messages": [
      {"role": "system", "content": "..."},
      {"role": "user", "content": "..."}
    ]
  },
  "parameters": {
    "result_format": "message",
    "incremental_output": true,
    "tools": [...]
  }
}
```

Key differences:
- Messages nested under `input.messages`
- Tools nested under `parameters.tools`
- Streaming enabled via `"incremental_output": true` + `X-DashScope-SSE: enable` header
- Tool format similar to OpenAI

**Quirks:**
- `X-DashScope-SSE: enable` header required for streaming
- Response format differs slightly — check `output.choices[0].message`
- Some models use different endpoints for different capabilities

---

## Provider Comparison Matrix

| Feature | Ollama | OpenAI | DeepSeek | Anthropic | Bedrock | Alibaba |
|---------|--------|--------|----------|-----------|---------|---------|
| Auth | None | Bearer token | Bearer token | x-api-key | AWS SigV4 | Bearer token |
| Streaming | NDJSON | SSE | SSE | SSE | Event stream | SSE |
| Tool calling | Varies by model | Full | Partial (V3 only) | Full | Full | Full |
| System prompt | In messages | In messages | In messages | Top-level field | Top-level array | In messages |
| Tool definition key | `function.parameters` | `function.parameters` | `function.parameters` | `input_schema` | `toolSpec.inputSchema.json` | `function.parameters` |
| Tool args format | Parsed object | Streamed JSON string | Streamed JSON string | Streamed JSON delta | Event stream | Streamed JSON string |
| Rate limits | None (local) | Per-tier | Aggressive | Per-model | Per-account | Per-model |
| Local | Yes | No | No | No | No | No |

## Adding a New Provider

1. Create `internal/provider/{name}.go`
2. Implement the `Provider` interface
3. Map the internal `ChatRequest` to the provider's message format
4. Parse the provider's streaming response into `ChatEvent` channel events
5. Handle auth, errors, and rate limiting
6. Register in the provider factory (`provider.go`)
7. Add config fields and document in this file
