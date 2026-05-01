<img src="docs/images/forge-title.svg" alt="FORGE" height="64"><img src="docs/images/forge-banner.png" alt="" width="180">

A terminal-based AI coding agent built in Go. Fast, beautiful, concurrent.</p>

Works with local LLMs (Ollama) and cloud providers (OpenAI, Anthropic, AWS Bedrock, DeepSeek).

```
  ⚡ forge • ollama/llama3
  ❯ fix the null pointer in main.go

  ● Read main.go
    main.go (23 lines)

  I found the issue on line 14...

  ● Write main.go
    main.go (23 lines written)

  Fixed. The nil pointer was caused by...
```

## Install

```bash
go install github.com/serjikisa/forge/cmd/forge@latest
```

Or build from source:

```bash
git clone https://github.com/serjikisa/forge.git
cd forge
make build
make install
```

Requires Go 1.26+.

## Quick Start

```bash
# Start chatting with your default model
forge chat

# Auto-approve all tool calls (no confirmation prompts)
forge chat --yes

# Log prompts and responses to a file
forge chat --log out/chat.txt

# Use a custom system prompt
forge chat --system-prompt "You are a Go expert. Always suggest idiomatic Go."

# Load system prompt from a file
forge chat --system-prompt-file ./prompts/reviewer.md

# Ask a one-off question
forge ask "explain this error" --file main.go

# Start the REST API server
forge serve --port 8080

# Use a specific provider
forge chat --provider openai --model gpt-4o
forge chat --provider anthropic --model claude-sonnet-5
forge chat --provider bedrock --model us.anthropic.claude-sonnet-4-20250514-v1:0
forge chat --provider deepseek --model deepseek-chat

# List available models
forge models
```

On first run, Forge will prompt you to select a provider and model.

## Features

- **Concurrent tool execution** — multiple file reads, searches, and commands run in parallel
- **Streaming responses** — tokens appear instantly as the model generates them
- **Rich terminal UI** — Kiro-style output with colored bullets, action verbs, and detail lines
- **REST API server** — `forge serve` exposes a JSON API for external tool integration
- **Multi-provider** — switch between local and cloud models with a flag
- **Safe by default** — destructive operations require confirmation, project boundary enforcement
- **Auto-approve mode** — `--yes` flag skips all confirmation prompts
- **Session persistence** — `/save` and `/resume` to continue conversations across sessions
- **Edit tool** — targeted string replacement in files, more efficient than full rewrites
- **Zero dependencies** — single static binary, only `golang.org/x/term` for terminal handling

## Providers

| Provider | Local | Tool Calling | Auth |
|----------|-------|-------------|------|
| Ollama | Yes | Varies by model | None |
| OpenAI | No | Full | `OPENAI_API_KEY` |
| Anthropic | No | Full | `ANTHROPIC_API_KEY` |
| AWS Bedrock | No | Full | AWS credentials (`~/.aws/credentials`) |
| DeepSeek | No | Partial (r1/r2 auto-disabled) | `DEEPSEEK_API_KEY` |

## Configuration

Config lives at `~/.forge/config.json`. Created automatically on first run.

```json
{
  "default_provider": "ollama",
  "max_concurrency": 5,
  "shell_timeout": 120,
  "providers": {
    "ollama": {
      "host": "http://localhost:11434",
      "model": "llama3"
    },
    "openai": {
      "model": "gpt-4o"
    },
    "anthropic": {
      "model": "claude-sonnet-5"
    },
    "bedrock": {
      "region": "us-west-2",
      "model": "us.anthropic.claude-sonnet-4-20250514-v1:0"
    },
    "deepseek": {
      "model": "deepseek-chat"
    }
  }
}
```

| Setting | Description | Default |
|---------|-------------|---------|
| `max_concurrency` | Max parallel tool executions | 5 |
| `shell_timeout` | Shell command timeout in seconds | 120 |

**Bedrock auth:** reads from `~/.aws/credentials` (uses `AWS_PROFILE` env var, defaults to `[default]`). Credentials are refreshed before each request.

API keys are read from environment variables — never stored in the config file.

See [docs/config.md](docs/config.md) for the full reference.

## Commands

| Command | Description |
|---------|-------------|
| `forge chat` | Interactive chat session (default) |
| `forge chat --yes` | Chat with auto-approved tool calls |
| `forge chat --log out/chat.txt` | Chat with prompt/response logging |
| `forge chat --system-prompt "..."` | Chat with a custom system prompt |
| `forge chat --system-prompt-file p.md` | Chat with system prompt from file |
| `forge ask "<prompt>"` | Single-shot query |
| `forge serve --port 8080` | Start REST API server |
| `forge models` | List available models |
| `forge version` | Print version |

**In-session commands:**

| Command | Description |
|---------|-------------|
| `/help` | Show available commands |
| `/clear` | Clear conversation history |
| `/save` | Save session to `~/.forge/sessions/` |
| `/resume` | Resume the last saved session |
| `/model` | Show current provider and model |
| `/model ls` | List available models |
| `/model <name>` | Switch to a different model |
| `/exit` | Exit forge |

## Documentation

- [Architecture](docs/architecture.md)
- [Configuration](docs/config.md)
- [Providers](docs/providers.md)
- [Tools](docs/tools.md)
- [Prompts](docs/prompts.md)
- [Contributing](docs/contributing.md)
- [Development](docs/DEVELOPMENT.md)
- [Backlog](docs/backlog.md)

## License

MIT
