// Package tool defines the Tool interface and registry, plus shared helpers for
// project boundary enforcement and path resolution used by all tool implementations.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
		&WebSearch{},
		&WebFetch{},
	}
}

// ProjectDir finds the project root: nearest .git parent or cwd.
// Result is cached after first call. Call ResetProjectDir() in tests.
var (
	projectDir     string
	projectDirOnce sync.Once
)

func ProjectDir() string {
	projectDirOnce.Do(func() {
		projectDir = findProjectDir()
	})
	return projectDir
}

// ResetProjectDir clears the cached project directory (for tests).
func ResetProjectDir() {
	projectDirOnce = sync.Once{}
	projectDir = ""
}

func findProjectDir() string {
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

// flexInt unmarshals both JSON numbers and quoted strings as int.
type flexInt int

func (f *flexInt) UnmarshalJSON(b []byte) error {
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		*f = flexInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("flexInt: cannot unmarshal %s", string(b))
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("flexInt: %q is not a number", s)
	}
	*f = flexInt(n)
	return nil
}
