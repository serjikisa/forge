# Tools

Built-in tools that the agent can invoke during a conversation.

## Tool Interface

```go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Execute(ctx context.Context, params json.RawMessage) (string, error)
    SafetyLevel() SafetyLevel
}

type SafetyLevel int
const (
    Safe              SafetyLevel = iota  // no side effects
    NeedsConfirmation                      // modifies files or system
    Dangerous                              // destructive, hard to reverse
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
{
  "path": "string (required) — absolute or relative file path",
  "offset": "int (optional) — line number to start reading from",
  "limit": "int (optional) — max lines to return"
}
```

**Returns:** File contents as a string. Returns an error if the file doesn't exist or isn't readable.

**Edge cases:**
- Binary files: detect via null bytes in first 512 bytes, return `"binary file, not displayed"`
- Large files: default limit of 500 lines, agent can request specific ranges
- Symlinks: follow symlinks, but don't follow outside the project directory

---

## write_file

Create a new file or overwrite an existing file.

| Field | Value |
|-------|-------|
| Safety | `NeedsConfirmation` |
| File | `internal/tool/file.go` |

**Parameters:**
```json
{
  "path": "string (required) — file path to write",
  "content": "string (required) — full file content"
}
```

**Returns:** Confirmation message with bytes written.

**Behavior:**
- Creates parent directories if they don't exist (`os.MkdirAll`)
- Shows a colored diff to the user before writing (if file exists)
- User must confirm before the write executes
- Preserves file permissions on overwrite

**Edge cases:**
- Writing outside project directory: blocked, return error
- Empty content: allowed (creates empty file)

---

## list_directory

List files and directories at a given path.

| Field | Value |
|-------|-------|
| Safety | `Safe` |
| File | `internal/tool/file.go` |

**Parameters:**
```json
{
  "path": "string (required) — directory path",
  "depth": "int (optional, default 1) — recursion depth",
  "include_hidden": "bool (optional, default false)"
}
```

**Returns:** Newline-separated list of entries with type indicators:
```
main.go
handler.go
internal/
  agent/
  provider/
docs/
  specs.md
```

**Edge cases:**
- Very large directories: cap at 200 entries, note truncation
- Permission denied: return error for that entry, continue listing others
- Symlinks: show but don't follow into

---

## shell_exec

Execute a shell command and return its output.

| Field | Value |
|-------|-------|
| Safety | `NeedsConfirmation` (elevated to `Dangerous` for destructive commands) |
| File | `internal/tool/shell.go` |

**Parameters:**
```json
{
  "command": "string (required) — shell command to execute",
  "working_dir": "string (optional) — directory to run in, defaults to project root"
}
```

**Returns:** Combined stdout and stderr, plus exit code.

**Behavior:**
- Runs via `os/exec` with the user's default shell
- Timeout: 30 seconds default (configurable)
- User sees the command and must confirm before execution
- Stdout and stderr are captured separately but returned combined
- Exit code included in result: `"exit code: 0"` or `"exit code: 1\nstderr: ..."`

**Dangerous command detection:**

Commands matching these patterns are elevated to `Dangerous` safety level and shown in red:
- `rm -rf`, `rm -r` (recursive delete)
- `git push --force`, `git reset --hard`
- `drop table`, `drop database`
- `chmod 777`, `chmod -R`
- `> /dev/sda`, `mkfs`
- `kill -9`, `killall`

**Edge cases:**
- Interactive commands (vim, less): not supported, return error
- Long-running commands: killed after timeout, partial output returned
- Commands that read stdin: not supported, stdin is closed

---

## search_code

Search for a regex pattern across files in the project.

| Field | Value |
|-------|-------|
| Safety | `Safe` |
| File | `internal/tool/search.go` |

**Parameters:**
```json
{
  "pattern": "string (required) — regex pattern",
  "path": "string (optional) — directory to search in, defaults to project root",
  "include": "string (optional) — file glob filter, e.g. '*.go'",
  "max_results": "int (optional, default 50)"
}
```

**Returns:** Matching lines with file path and line number:
```
main.go:14: func handleError(err error) {
main.go:28:     return fmt.Errorf("handler: %w", err)
utils.go:5: var ErrNotFound = errors.New("not found")
```

**Behavior:**
- Uses `regexp` package (stdlib)
- Walks directory tree via `filepath.WalkDir`
- Skips binary files, `.git/`, `node_modules/`, and other common ignore patterns
- Respects `.gitignore` if present (basic glob matching)
- Case-insensitive option via `(?i)` prefix in pattern

**Edge cases:**
- Invalid regex: return a clear error with the regex syntax issue
- No matches: return `"no matches found"`
- Very large repos: bounded by `max_results`, stops walking after limit

---

## web_search

Search the web. Disabled by default.

| Field | Value |
|-------|-------|
| Safety | `Safe` |
| File | `internal/tool/search.go` |

**Parameters:**
```json
{
  "query": "string (required) — search query",
  "max_results": "int (optional, default 5)"
}
```

**Returns:** List of results with title, URL, and snippet.

**Configuration required:**
```json
{
  "search_provider": "brave",
  "search_api_key": "${BRAVE_API_KEY}"
}
```

Supported search backends:
- **SearXNG** — self-hosted, no API key needed, set `search_url`
- **Brave Search API** — requires API key
- **Google Custom Search** — requires API key + search engine ID

All use `net/http` — fully stdlib compatible.

---

## Tool Registration

Tools are registered in a central registry that accepts config for conditional registration:

```go
func Registry(cfg *config.Config) []Tool {
    tools := []Tool{
        &ReadFile{},
        &WriteFile{},
        &ListDirectory{},
        &ShellExec{Timeout: 30 * time.Second},
        &SearchCode{MaxResults: 50},
    }
    if cfg.SearchProvider != "" {
        tools = append(tools, &WebSearch{
            Provider: cfg.SearchProvider,
            APIKey:   cfg.SearchAPIKey,
            URL:      cfg.SearchURL,
        })
    }
    return tools
}
```

The agent passes this list to the provider, which converts each tool's `Schema()` to the provider-specific format.

## Project Directory

The **project directory** is the security boundary for all file operations. Defined as:

1. The nearest parent directory containing a `.git/` directory (git root), OR
2. If no `.git/` found, the current working directory at Forge startup

All file paths in tool parameters are resolved relative to the project directory. Tools reject any path that resolves outside it (including via symlinks — the resolved target is checked).

Stored in the agent context and passed to tools at execution time.

## `--file` Flag

The `forge ask` command supports a `--file` flag:

```bash
forge ask "explain this error" --file main.go
```

Behavior: reads the file contents and appends them to the user message as a code block:

```
explain this error

---
File: main.go
```go
<file contents here>
`` `
```

Multiple `--file` flags are supported. Files are read before sending the request.

## External Tools

Users can extend Forge with custom tools — any executable that reads JSON from stdin and writes JSON to stdout.

Registered in `config.json` under `external_tools` (see config.md). The agent sees them alongside built-in tools.

**Execution flow:**
1. Agent requests the external tool
2. Permission prompt shown to user (always `Ask` by default)
3. Parameters serialized as JSON, piped to the command's stdin
4. Command runs with a 30s timeout (same as `shell_exec`)
5. Stdout is captured as the tool result
6. Non-zero exit code → error result sent back to agent

**Example external tool (`scripts/deploy.sh`):**
```bash
#!/bin/bash
# Reads JSON from stdin: {"env": "staging"}
ENV=$(echo "$1" | jq -r '.env')
./deploy --env "$ENV" 2>&1
```

External tools inherit the project directory as their working directory.
