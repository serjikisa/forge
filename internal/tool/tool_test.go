package tool

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, input, want string
	}{
		{"tilde alone", "~", home},
		{"tilde slash foo", "~/foo", filepath.Join(home, "foo")},
		{"no tilde", "/usr/bin", "/usr/bin"},
		{"empty", "", ""},
		{"relative", "foo/bar", "foo/bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expandHome(tt.input); got != tt.want {
				t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInProject(t *testing.T) {
	ResetProjectDir()
	t.Run("relative path inside project", func(t *testing.T) {
		if !inProject("tool.go") {
			t.Error("expected tool.go to be in project")
		}
	})

	t.Run("absolute path outside project", func(t *testing.T) {
		if inProject("/etc/passwd") {
			t.Error("expected /etc/passwd to be outside project")
		}
	})

	t.Run("symlink inside project", func(t *testing.T) {
		tmp := t.TempDir()
		target := filepath.Join(tmp, "target.txt")
		if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(tmp, "link.txt")
		if err := os.Symlink(target, link); err != nil {
			t.Skip("cannot create symlink:", err)
		}
		// Both target and link are in tmp, which is outside the project
		if inProject(link) {
			t.Error("expected symlink in temp dir to be outside project")
		}
	})

	t.Run("nonexistent file for write_file", func(t *testing.T) {
		// A nonexistent file under the project root should still be considered in-project
		if !inProject("nonexistent_file_abc123.txt") {
			t.Error("expected nonexistent relative path to be in project")
		}
	})
}

func TestProjectDir(t *testing.T) {
	ResetProjectDir()
	dir := ProjectDir()
	if dir == "" {
		t.Fatal("ProjectDir() returned empty string")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("ProjectDir() %q does not exist: %v", dir, err)
	}
	if !info.IsDir() {
		t.Fatalf("ProjectDir() %q is not a directory", dir)
	}
}

func TestRegistry(t *testing.T) {
	tools := Registry()
	if len(tools) != 7 {
		t.Fatalf("expected 7 tools, got %d", len(tools))
	}
	got := make([]string, len(tools))
	for i, tl := range tools {
		got[i] = tl.Name()
	}
	sort.Strings(got)
	want := []string{"list_directory", "read_file", "search_code", "shell_exec", "web_fetch", "web_search", "write_file"}
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
