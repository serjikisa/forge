# Configuration

## File Location

```
~/.forge/config.json
```

Created automatically on first run with defaults.

## Full Example

```json
{
  "default_provider": "ollama",
  "theme": "vibrant",
  "log_level": "info",
  "log_format": "pretty",
  "max_concurrency": 5,
  "shell_timeout": 120,
  "providers": {
    "ollama": {
      "host": "http://localhost:11434",
      "model": "llama3"
    },
    "openai": {
      "base_url": "https://api.openai.com/v1",
      "model": "gpt-4o"
    },
    "anthropic": {
      "model": "claude-sonnet-5"
    },
    "bedrock": {
      "region": "us-east-1",
      "model": "us.anthropic.claude-sonnet-4-20250514-v1:0"
    },
    "deepseek": {
      "base_url": "https://api.deepseek.com/v1",
      "model": "deepseek-chat"
    }
  },
  "model_prompts": {
    "llama3": "You are a senior Go developer. Prefer idiomatic Go patterns."
  }
}
```

## Field Reference

### Top-Level

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `default_provider` | string | `"ollama"` | Provider used when no `--provider` flag is given |
| `theme` | string | `"vibrant"` | Color theme (reserved for future use) |
| `log_level` | string | `"info"` | Log level: `debug`, `info`, `warn`, `error` |
| `log_format` | string | `"pretty"` | Log format: `pretty`, `text`, `json` |
| `max_concurrency` | int | `5` | Max parallel tool executions |
| `shell_timeout` | int | `120` | Shell command timeout in seconds |
| `model_prompts` | map | `{}` | Per-model system prompt overrides |

### Provider Fields

**Ollama:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `host` | string | `"http://localhost:11434"` | Ollama server URL |
| `model` | string | auto-detect | Default model (first installed if empty) |

**OpenAI:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | `"https://api.openai.com/v1"` | API base URL |
| `model` | string | `"gpt-4o"` | Default model |

API key via `OPENAI_API_KEY` environment variable.

**Anthropic:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `model` | string | `"claude-sonnet-5"` | Default model |

API key via `ANTHROPIC_API_KEY` environment variable.

**AWS Bedrock:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `region` | string | `"us-east-1"` | AWS region |
| `model` | string | — | Model ID (e.g. `us.anthropic.claude-sonnet-4-20250514-v1:0`) |

Auth via `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` environment variables, or `~/.aws/credentials` file (uses `AWS_PROFILE`, defaults to `[default]`). Credentials are refreshed before each request.

**DeepSeek:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | `"https://api.deepseek.com/v1"` | API base URL |
| `model` | string | `"deepseek-chat"` | Default model |

API key via `DEEPSEEK_API_KEY` environment variable. Uses OpenAI-compatible API.

## Environment Variables

These override config file values:

| Variable | Overrides |
|----------|-----------|
| `FORGE_PROVIDER` | `default_provider` |
| `FORGE_MODEL` | Active provider's `model` |
| `FORGE_LOG_LEVEL` | `log_level` |
| `FORGE_LOG_FORMAT` | `log_format` |

API keys are always read from environment variables. The `${VAR}` syntax in config values is expanded via `os.ExpandEnv` at load time.

## Precedence

```
CLI flag  >  Environment variable  >  Config file  >  Built-in default
```

### System Prompt Precedence

```
--system-prompt / --system-prompt-file  >  model_prompts[model]  >  built-in default
```

```bash
forge chat --system-prompt "You are a security auditor. Focus on vulnerabilities."
forge chat --system-prompt-file ./prompts/code-reviewer.md
```

## Local Storage

```
~/.forge/
├── config.json      -- user configuration (this file)
└── sessions/        -- conversation history (via /save command)
    └── *.json
```
