package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type EditFile struct{}

func (e *EditFile) Name() string        { return "edit_file" }
func (e *EditFile) Description() string { return "Replace an exact string in a file with new content. More token-efficient than rewriting the whole file." }
func (e *EditFile) Safety() SafetyLevel { return NeedsConfirmation }
func (e *EditFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"file path"},"old_string":{"type":"string","description":"exact text to find (must be unique in the file)"},"new_string":{"type":"string","description":"replacement text"}},"required":["path","old_string","new_string"]}`)
}

func (e *EditFile) Execute(_ context.Context, params json.RawMessage) (string, error) {
	var p struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
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

	content := string(data)
	count := strings.Count(content, p.OldString)
	if count == 0 {
		return "", fmt.Errorf("old_string not found in %s", p.Path)
	}
	if count > 1 {
		return "", fmt.Errorf("old_string appears %d times in %s — must be unique", count, p.Path)
	}

	newContent := strings.Replace(content, p.OldString, p.NewString, 1)
	if err := os.WriteFile(p.Path, []byte(newContent), 0o644); err != nil {
		return "", err
	}

	return fmt.Sprintf("edited %s (replaced %d chars with %d chars)", p.Path, len(p.OldString), len(p.NewString)), nil
}
