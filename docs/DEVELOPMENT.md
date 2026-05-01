# Development Guidelines

## Build

```bash
go build ./...
```

## Test

```bash
go test ./... -count=1
```

## Race Condition Check

Always run with the race detector before considering work complete:

```bash
go test -race ./... -count=1
```

## Before Marking a Task as Done

Every change must pass all three checks:

```bash
go build ./...              # 1. compiles
go test ./... -count=1      # 2. tests pass
go test -race ./... -count=1 # 3. no race conditions
```

If any check fails, fix the issue before proceeding.

## Project Structure

```
forge/
├── cmd/forge/         # CLI entry point
├── internal/
│   ├── agent/         # Agent loop, slash commands, tool execution
│   ├── config/        # Configuration loading
│   ├── provider/      # LLM provider interface and implementations
│   ├── tool/          # Tool interface and implementations
│   └── tui/           # Terminal UI, spinner, colors, readline
└── docs/              # Documentation
```

## Writing Tests

- Test pure functions directly (truncate, summarizeArgs, isDangerous, color functions).
- Use stub/mock implementations for interfaces (Provider, Tool) to test logic without network calls.
- Test state machines (spinner, noTools fallback) by verifying state transitions.
- Test concurrency with goroutines and sync.WaitGroup to catch races.
- Use table-driven tests for functions with multiple input/output cases.

## Adding a New Provider

1. Implement the `provider.Provider` interface.
2. Optionally implement `provider.ModelSwitcher` for runtime model switching.
3. Add a case in `cmd/forge/main.go` `newProvider()`.
4. Add tests in `internal/provider/`.

## Adding a New Tool

1. Implement the `tool.Tool` interface.
2. Register it in `tool.Registry()`.
3. Set the appropriate `SafetyLevel`.
4. Add tests for the tool's `Execute` method.
