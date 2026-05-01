// file.go implements the read_file, write_file, and list_directory tools for
// filesystem access with binary detection and go.mod protection.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --- read_file ---

type ReadFile struct{}

func (r *ReadFile) Name() string        { return "read_file" }
func (r *ReadFile) Description() string { return "Read the contents of a file at the given path" }
func (r *ReadFile) Safety() SafetyLevel { return Safe }
func (r *ReadFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"file path"}},"required":["path"]}`)
}

func (r *ReadFile) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct{ Path string `json:"path"` }
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}
	p.Path = expandHome(p.Path)
	if !inProject(p.Path) {
		return "", fmt.Errorf("path %q is outside project directory", p.Path)
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return "", err
	}
	// Binary detection
	for _, b := range data[:min(len(data), 512)] {
		if b == 0 {
			return "binary file, not displayed", nil
		}
	}
	return string(data), nil
}

// --- write_file ---

type WriteFile struct{}

func (w *WriteFile) Name() string        { return "write_file" }
func (w *WriteFile) Description() string { return "Create or overwrite a file with the given content" }
func (w *WriteFile) Safety() SafetyLevel { return NeedsConfirmation }
func (w *WriteFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"file path"},"content":{"type":"string","description":"file content"}},"required":["path","content"]}`)
}

func (w *WriteFile) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}
	p.Path = expandHome(p.Path)
	if !inProject(p.Path) {
		return "", fmt.Errorf("path %q is outside project directory", p.Path)
	}
	// Prevent creating go.mod/go.sum in subdirectories (breaks module resolution)
	base := filepath.Base(p.Path)
	if base == "go.mod" || base == "go.sum" {
		abs, _ := filepath.Abs(p.Path)
		if filepath.Dir(abs) != ProjectDir() {
			return "", fmt.Errorf("cannot create %s in subdirectory — would break module resolution", base)
		}
	}
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
}

// --- list_directory ---

type ListDir struct{}

func (l *ListDir) Name() string        { return "list_directory" }
func (l *ListDir) Description() string { return "List files and directories at a given path" }
func (l *ListDir) Safety() SafetyLevel { return Safe }
func (l *ListDir) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"directory path"}},"required":["path"]}`)
}

func (l *ListDir) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct{ Path string `json:"path"` }
	if err := json.Unmarshal(params, &p); err != nil {
		return "", err
	}
	if p.Path == "" {
		p.Path = "."
	}
	p.Path = expandHome(p.Path)
	if !inProject(p.Path) {
		return "", fmt.Errorf("path %q is outside project directory", p.Path)
	}

	entries, err := os.ReadDir(p.Path)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for i, e := range entries {
		if i >= 200 {
			fmt.Fprintf(&b, "... truncated (%d total)\n", len(entries))
			break
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		b.WriteString(name)
		b.WriteByte('\n')
	}
	return b.String(), nil
}
