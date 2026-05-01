<img src="docs/images/forge-title.svg" alt="FORGE" height="64"><img src="docs/images/forge-banner.png" alt="" width="180">

A terminal-based AI coding agent built in Go. Fast, beautiful, concurrent.</p>

Works with local LLMs (Ollama) and cloud providers (OpenAI, Anthropic, AWS Bedrock, DeepSeek, Alibaba Cloud).

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

**macOS:**
```bash
brew tap forge-cli/tap && brew install forge
```

**Windows:**
```powershell
scoop bucket add forge https://github.com/forge-cli/scoop-bucket
scoop install forge
```

**One-liner:**
```bash
curl -fsSL https://forge-cli.dev/install.sh | sh
```

## Quick Start

```bash
# Start chatting with your default model
forge chat

# Ask a one-off question
forge ask "explain this error" --file main.go

# Use a specific provider
forge chat --provider openai --model gpt-4o

# List available models
forge models
```

On first run, Forge will prompt you to select a provider and model.

## Features

- **Concurrent tool execution** — multiple file reads, searches, and commands run in parallel
- **Streaming responses** — tokens appear instantly as the model generates them
- **Rich terminal UI** — colored output, animated spinners, syntax highlighting, markdown rendering
- **Multi-provider** — switch between local and cloud models with a flag
- **Safe by default** — destructive operations require confirmation, file writes show diffs
- **Zero dependencies** — single static binary, built entirely with Go's standard library

## Providers

| Provider | Local | Tool Calling | Auth |
|----------|-------|-------------|------|
| Ollama | Yes | Varies by model | None |
| OpenAI | No | Full | API key |
| Anthropic | No | Full | API key |
| AWS Bedrock | No | Full | AWS credentials |
| DeepSeek | No | Partial | API key |
| Alibaba (DashScope) | No | Full | API key |

## Configuration

Config lives at `~/.forge/config.json`. Set your default provider and model:

```json
{
  "default_provider": "ollama",
  "providers": {
    "ollama": {
      "host": "http://localhost:11434",
      "model": "llama3"
    }
  }
}
```

API keys are read from environment variables — never stored in the config file.

See [docs/config.md](docs/config.md) for the full reference.

## Commands

| Command | Description |
|---------|-------------|
| `forge chat` | Interactive chat session (default) |
| `forge ask "<prompt>"` | Single-shot query |
| `forge models` | List available models |
| `forge config` | Manage settings |
| `forge update` | Self-update to latest version |
| `forge version` | Print version |

## Build from Source

```bash
git clone https://github.com/forge-cli/forge.git
cd forge
make build
./bin/forge chat
```

Requires Go 1.22+.

## Documentation

- [Architecture](docs/architecture.md)
- [Providers](docs/providers.md)
- [Prompts](docs/prompts.md)
- [Tools](docs/tools.md)
- [Configuration](docs/config.md)
- [Contributing](docs/contributing.md)
- [Specs](docs/specs.md)

## License

MIT
