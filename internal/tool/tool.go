package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type SafetyLevel int

const (
	Safe SafetyLevel = iota
	NeedsConfirmation
	Dangerous
)

type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
	Execute(ctx context.Context, params json.RawMessage) (string, error)
	Safety() SafetyLevel
}

func Registry() []Tool {
	return []Tool{
		&ReadFile{},
		&WriteFile{},
		&ListDir{},
		&ShellExec{},
		&SearchCode{},
	}
}

// ProjectDir finds the project root: nearest .git parent or cwd.
func ProjectDir() string {
	dir, _ := os.Getwd()
	cur := dir
	for {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return dir
}

func inProject(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	// Resolve symlinks for existing files
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolved = abs // file may not exist yet (write_file)
	}
	root := ProjectDir()
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return false
	}
	// Must not start with ".." (which means it's outside the root)
	return !strings.HasPrefix(rel, "..")
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, path[1:])
		}
	}
	return path
}
