# Configuration

## File Location

```
~/.forge/config.json
```

Created automatically on first run via the setup wizard.

## Full Example

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
  },
  "search_provider": "",
  "search_api_key": "",
  "search_url": ""
}
```

## Field Reference

### Top-Level

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `default_provider` | string | `"ollama"` | Provider used when no `--provider` flag is given |
| `theme` | string | `"vibrant"` | Color theme: `vibrant`, `minimal`, `mono` |
| `log_level` | string | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `log_format` | string | `"text"` | Log format: `text`, `json` |
| `auto_update` | bool | `true` | Check for new versions on startup |
| `max_concurrency` | int | `5` | Max parallel tool executions |

### Provider Fields

**Ollama:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `host` | string | `"http://localhost:11434"` | Ollama server URL |
| `model` | string | `"llama3"` | Default model |

**OpenAI:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | `"https://api.openai.com/v1"` | API base URL |
| `api_key` | string | — | API key (use `${OPENAI_API_KEY}`) |
| `model` | string | `"gpt-4o"` | Default model |

**Anthropic:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `api_key` | string | — | API key (use `${ANTHROPIC_API_KEY}`) |
| `model` | string | `"claude-sonnet-4-20250514"` | Default model |

**AWS Bedrock:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `region` | string | `"us-east-1"` | AWS region |
| `model` | string | — | Model ID (e.g. `anthropic.claude-sonnet-4-20250514-v1:0`) |

Bedrock uses standard AWS credentials (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`) from the environment.

**DeepSeek:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | `"https://api.deepseek.com/v1"` | API base URL |
| `api_key` | string | — | API key (use `${DEEPSEEK_API_KEY}`) |
| `model` | string | `"deepseek-v3"` | Default model |

**Alibaba Cloud (DashScope):**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | `"https://dashscope.aliyuncs.com/api/v1"` | API base URL |
| `api_key` | string | — | API key (use `${DASHSCOPE_API_KEY}`) |
| `model` | string | `"qwen-max"` | Default model |

### Search Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `search_provider` | string | `""` (disabled) | `brave`, `google`, or `searxng` |
| `search_api_key` | string | `""` | API key for Brave or Google search |
| `search_url` | string | `""` | SearXNG instance URL (e.g. `http://localhost:8080`) |

### Permissions

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `permissions.file_read` | string | `"allow"` | `allow`, `ask`, or `deny` |
| `permissions.file_write` | string | `"ask"` | `allow`, `ask`, or `deny` |
| `permissions.shell` | string | `"ask"` | `allow`, `ask`, or `deny` |
| `permissions.web` | string | `"ask"` | `allow`, `ask`, or `deny` |
| `permissions.external` | string | `"ask"` | `allow`, `ask`, or `deny` |

Override per session with flags: `--allow-write`, `--allow-shell`, `--allow-web`, `--allow-all`.

### External Tools

User-defined tools that Forge can invoke. Each entry is an executable that accepts JSON stdin and returns JSON stdout:

```json
{
  "external_tools": [
    {
      "name": "deploy",
      "description": "Deploy to staging environment",
      "command": "./scripts/deploy.sh",
      "schema": { "type": "object", "properties": { "env": { "type": "string" } } },
      "permission": "ask"
    }
  ]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Tool name (used by the LLM) |
| `description` | string | yes | What the tool does (shown to LLM) |
| `command` | string | yes | Path to executable |
| `schema` | object | yes | JSON Schema for parameters |
| `permission` | string | no | `ask` (default) or `allow` |

## Environment Variables

These override config file values:

| Variable | Overrides |
|----------|-----------|
| `FORGE_PROVIDER` | `default_provider` |
| `FORGE_MODEL` | Active provider's `model` |
| `FORGE_LOG_LEVEL` | `log_level` |
| `FORGE_NO_UPDATE_CHECK=1` | Disables auto-update check |

API keys are always read from environment variables — the `${VAR}` syntax in config is expanded via `os.ExpandEnv` at load time.

## Precedence

```
CLI flag  >  Environment variable  >  Config file  >  Built-in default
```

### System Prompt Precedence

```
--system-prompt / --system-prompt-file  >  model_prompts[model]  >  built-in default
```

Use `--system-prompt` for inline text or `--system-prompt-file` to load from a file:

```bash
forge chat --system-prompt "You are a security auditor. Focus on vulnerabilities."
forge chat --system-prompt-file ./prompts/code-reviewer.md
```

For persistent per-model prompts, use `model_prompts` in config.json:

```json
{
  "model_prompts": {
    "llama3": "You are a senior Go developer. Prefer idiomatic Go patterns.",
    "qwen2.5-coder:latest": "Always use tools proactively. Never ask the user to paste code."
  }
}
```

## Local Storage

```
~/.forge/
├── config.json      -- user configuration (this file)
├── state.json       -- runtime state (auto-managed, don't edit)
└── sessions/        -- conversation history
    └── *.json
```

`state.json` is managed by Forge internally:
```json
{
  "last_update_check": "2026-05-01T10:00:00Z",
  "latest_known_version": "v1.4.0",
  "last_session_id": "2026-05-01_14-30"
}
```

## CLI Config Commands

```bash
# Show current config
forge config

# Set a value
forge config set default_provider openai
forge config set theme minimal
forge config set auto_update false

# First-run setup wizard
forge config init
```
