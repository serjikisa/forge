# Tools

Built-in tools that the agent can invoke during a conversation.

## Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Execute(ctx context.Context, params json.RawMessage) (string, error)
    Safety() SafetyLevel
}

type SafetyLevel int
const (
    Safe              SafetyLevel = iota
    NeedsConfirmation
)
```

## read_file

Read the contents of a file.

| Field | Value |
|-------|-------|
| Safety | `Safe` |
| File | `internal/tool/file.go` |

**Parameters:**
```json
{"path": "string (required)"}
```

**Behavior:**
- Binary detection via null bytes in first 512 bytes
- Returns `"binary file, not displayed"` for binary files
- Rejects paths outside project directory (`.git` root)
- Expands `~` to home directory

---

## write_file

Create or overwrite a file with the given content.

| Field | Value |
|-------|-------|
| Safety | `NeedsConfirmation` |
| File | `internal/tool/file.go` |

**Parameters:**
```json
{"path": "string (required)", "content": "string (required)"}
```

**Behavior:**
- Creates parent directories if needed
- Rejects paths outside project directory
- Blocks creating `go.mod`/`go.sum` in subdirectories

---

## edit_file

Replace an exact string in a file with new content. More token-efficient than rewriting entire files.

| Field | Value |
|-------|-------|
| Safety | `NeedsConfirmation` |
| File | `internal/tool/edit.go` |

**Parameters:**
```json
{"path": "string (required)", "old_string": "string (required)", "new_string": "string (required)"}
```

**Behavior:**
- `old_string` must appear exactly once in the file (uniqueness enforced)
- Rejects paths outside project directory
- Returns error if string not found or appears multiple times

---

## list_directory

List files and directories at a given path.

| Field | Value |
|-------|-------|
| Safety | `Safe` |
| File | `internal/tool/file.go` |

**Parameters:**
```json
{"path": "string (required)"}
```

**Behavior:**
- Directories shown with trailing `/`
- Capped at 200 entries with truncation note
- Rejects paths outside project directory

---

## shell_exec

Execute a shell command and return its output.

| Field | Value |
|-------|-------|
| Safety | `NeedsConfirmation` |
| File | `internal/tool/shell.go` |

**Parameters:**
```json
{"command": "string (required)"}
```

**Behavior:**
- Runs via `sh -c` (or `cmd /C` on Windows)
- Configurable timeout (default 120s, via `shell_timeout` in config)
- Returns combined stdout + stderr
- Non-zero exit codes returned as part of output (not an error)
- Advisory boundary check for commands targeting sensitive paths (`/etc/`, `/root/`, `/var/`, `/tmp/`)
- Dangerous command patterns highlighted in red in TUI: `rm -rf`, `git push --force`, `git reset --hard`, `drop table`, `chmod 777`, `mkfs`, `killall`

---

## search_code

Search for a regex pattern across files in the project.

| Field | Value |
|-------|-------|
| Safety | `Safe` |
| File | `internal/tool/search.go` |

**Parameters:**
```json
{"pattern": "string (required)", "path": "string (optional)", "include": "string (optional)"}
```

**Behavior:**
- Uses Go's `regexp` package
- Walks directory tree via `filepath.WalkDir`
- Skips: `.git/`, `node_modules/`, `vendor/`, `dist/`, `build/`, `.cache/`, `target/`
- Skips binary files
- Returns matches as `file:line: content`
- Capped at 50 results
- Supports glob filter via `include` (e.g. `"*.go"`)

---

## web_search

Search the web using DuckDuckGo.

| Field | Value |
|-------|-------|
| Safety | `Safe` |
| File | `internal/tool/web_search.go` |

**Parameters:**
```json
{"query": "string (required)", "num_results": "int (optional, default 5)"}
```

**Behavior:**
- Uses DuckDuckGo's HTML lite endpoint (no API key required)
- Parses results from HTML response
- Returns numbered results with title, URL, and snippet

---

## web_fetch

Fetch a URL and return its text content.

| Field | Value |
|-------|-------|
| Safety | `Safe` |
| File | `internal/tool/web_fetch.go` |

**Parameters:**
```json
{"url": "string (required)", "max_length": "int (optional, default 8000)"}
```

**Behavior:**
- Strips HTML tags, scripts, styles
- Converts block elements to newlines
- Decodes HTML entities
- Truncates at `max_length` characters
- Auto-prepends `https://` if no scheme provided

---

## Tool Registration

```go
func Registry(opts ...RegistryOptions) []Tool {
    return []Tool{
        &ReadFile{},
        &WriteFile{},
        &EditFile{},
        &ListDir{},
        &ShellExec{timeout: opts.ShellTimeout},
        &SearchCode{},
        &WebSearch{},
        &WebFetch{},
    }
}
```

## Project Directory

The **project directory** is the security boundary for file operations. Defined as:

1. The nearest parent directory containing `.git/`, OR
2. The current working directory if no `.git/` found

All file paths are resolved relative to the project directory. Tools reject any path that resolves outside it (symlinks are resolved and checked).
