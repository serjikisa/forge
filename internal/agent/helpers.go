package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/serjikisa/forge/internal/provider"
	"github.com/serjikisa/forge/internal/tool"
	"github.com/serjikisa/forge/internal/tui"
)

func (a *Agent) toolDefs() []provider.ToolDef {
	defs := make([]provider.ToolDef, len(a.tools))
	for i, t := range a.tools {
		defs[i] = provider.ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema(),
		}
	}
	return defs
}

func summarizeArgs(args json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(args, &m) == nil {
		if p, ok := m["path"]; ok {
			return fmt.Sprintf("%v", p)
		}
		if c, ok := m["command"]; ok {
			return truncate(fmt.Sprintf("%v", c), 60)
		}
		if p, ok := m["pattern"]; ok {
			return fmt.Sprintf("%v", p)
		}
	}
	return truncate(string(args), 60)
}

// toolDetail computes a Kiro-style detail string for a completed tool call.
func toolDetail(name string, args json.RawMessage, result string) string {
	var m map[string]any
	json.Unmarshal(args, &m)

	switch name {
	case "read_file":
		lines := strings.Count(result, "\n")
		if p, ok := m["path"]; ok {
			return fmt.Sprintf("%v (%d lines)", shortName(fmt.Sprintf("%v", p)), lines)
		}
		return fmt.Sprintf("(%d lines)", lines)
	case "list_directory":
		entries := strings.Count(strings.TrimSpace(result), "\n") + 1
		if strings.TrimSpace(result) == "" {
			entries = 0
		}
		return fmt.Sprintf("(%d entries)", entries)
	case "write_file":
		lines := 0
		if c, ok := m["content"]; ok {
			lines = strings.Count(fmt.Sprintf("%v", c), "\n") + 1
		}
		if p, ok := m["path"]; ok {
			return fmt.Sprintf("%v (%d lines written)", shortName(fmt.Sprintf("%v", p)), lines)
		}
		return fmt.Sprintf("(%d lines written)", lines)
	case "shell_exec":
		out := strings.TrimSpace(result)
		if len(out) > 80 {
			out = out[:80] + "..."
		}
		if out == "" {
			return "(no output)"
		}
		return ""
	case "search_code":
		if strings.TrimSpace(result) == "" || result == "no matches found" {
			return "(no matches)"
		}
		matches := strings.Count(strings.TrimSpace(result), "\n") + 1
		return fmt.Sprintf("(%d matches)", matches)
	}
	return ""
}

func isDangerous(t tool.Tool, tc provider.ToolCall) bool {
	if t.Name() != "shell_exec" {
		return false
	}
	var p struct{ Command string `json:"command"` }
	json.Unmarshal(tc.Arguments, &p)
	lower := strings.ToLower(p.Command)
	for _, pat := range []string{"rm -rf", "rm -r", "git push --force", "git reset --hard", "drop table", "chmod 777"} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

func shortName(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func formatSize(bytes int64) string {
	const gb = 1024 * 1024 * 1024
	const mb = 1024 * 1024
	if bytes >= gb {
		return fmt.Sprintf("%.1fGB", float64(bytes)/float64(gb))
	}
	return fmt.Sprintf("%.0fMB", float64(bytes)/float64(mb))
}

func formatModified(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	days := int(time.Since(t).Hours() / 24)
	if days == 0 {
		return "today"
	}
	if days == 1 {
		return "yesterday"
	}
	return fmt.Sprintf("%d days ago", days)
}

// buildToolNames creates a resolver map for text tool call parsing.
func (a *Agent) buildToolNames() map[string]string {
	toolNames := make(map[string]string, len(a.toolMap)*2)
	for name, t := range a.toolMap {
		toolNames[name] = name
		toolNames[strings.ToLower(t.Description())] = name
	}
	return toolNames
}

// Ensure tui import is used
var _ = tui.Red
