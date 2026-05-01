# Contributing

## Prerequisites

- Go 1.22+
- Git
- An LLM provider (Ollama for local testing)

## Setup

```bash
git clone https://github.com/forge-cli/forge.git
cd forge
make build
make test
```

## Development Workflow

1. Create a branch: `git checkout -b feature/my-feature`
2. Make changes
3. Run tests: `make test` (always includes `-race`)
4. Run lint: `make lint`
5. Commit with a clear message
6. Open a PR

## Code Style

- **Standard library only** — do not add external dependencies
- Follow Go conventions: `gofmt`, `go vet`, effective Go
- Domain-based package layout (see architecture.md)
- Interfaces for testability — providers and tools are interfaces
- `context.Context` as first parameter on anything that does I/O
- Errors wrapped with `fmt.Errorf("context: %w", err)`
- No globals — pass dependencies explicitly

## Adding a Provider

1. Create `internal/provider/{name}.go`
2. Implement the `Provider` interface:
   ```go
   type Provider interface {
       ChatCompletion(ctx context.Context, req ChatRequest) (<-chan ChatEvent, error)
       ListModels(ctx context.Context) ([]Model, error)
       Name() string
       SupportsToolCalling() bool
   }
   ```
3. Map `ChatRequest` to the provider's API format
4. Parse streaming responses into `ChatEvent` channel
5. Register in the factory function in `provider.go`
6. Add config fields to `config.go`
7. Add tests using `httptest`
8. Document in `docs/providers.md`

## Adding a Tool

1. Create or edit the appropriate file in `internal/tool/`
2. Implement the `Tool` interface:
   ```go
   type Tool interface {
       Name() string
       Description() string
       Schema() json.RawMessage
       Execute(ctx context.Context, params json.RawMessage) (string, error)
       SafetyLevel() SafetyLevel
   }
   ```
3. Register in `tool.Registry()`
4. Add tests
5. Document in `docs/tools.md`

## Testing

```bash
# Run all tests with race detector
make test

# Run specific package
go test -race -v ./internal/agent/...

# Run specific test
go test -race -v -run TestParseCommand ./internal/...

# Coverage report
make cover
open coverage.html
```

- Table-driven tests for deterministic logic
- `httptest` for provider HTTP tests
- Mock providers/tools for agent loop tests
- `-race` flag is mandatory — CI will reject without it
- Aim for 80%+ coverage on `internal/agent/` and `internal/provider/`

## Commit Messages

```
component: short description

Longer explanation if needed. What changed and why.
```

Examples:
```
provider/openai: handle streaming tool call fragments
agent: add context compaction when window exceeds 80%
tui: fix spinner animation on Windows
```

## PR Guidelines

- One feature or fix per PR
- Include tests for new functionality
- Update relevant docs if behavior changes
- Keep PRs small — easier to review, faster to merge
