package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- ShellExec tests ---

func TestShellExec_Happy(t *testing.T) {
	s := &ShellExec{}
	out, err := s.Execute(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected 'hello' in output, got %q", out)
	}
}

func TestShellExec_Stderr(t *testing.T) {
	s := &ShellExec{}
	out, err := s.Execute(context.Background(), json.RawMessage(`{"command":"echo err >&2"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "err") {
		t.Errorf("expected 'err' in output, got %q", out)
	}
}

func TestShellExec_ExitCode(t *testing.T) {
	s := &ShellExec{}
	out, err := s.Execute(context.Background(), json.RawMessage(`{"command":"sh -c 'exit 1'"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit code:") {
		t.Errorf("expected 'exit code:' in output, got %q", out)
	}
}

func TestShellExec_IsDangerous(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"rm -rf /", true},
		{"rm -r ./src", true},
		{"git push --force", true},
		{"git reset --hard", true},
		{"drop table", true},
		{"drop database", true},
		{"chmod 777", true},
		{"mkfs", true},
		{"killall", true},
		{"ls -la", false},
		{"echo hello", false},
	}
	for _, tt := range tests {
		t.Run(tt.cmd, func(t *testing.T) {
			if got := isDangerous(tt.cmd); got != tt.want {
				t.Errorf("isDangerous(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestShellExec_InvalidJSON(t *testing.T) {
	s := &ShellExec{}
	_, err := s.Execute(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- SearchCode tests ---

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	ResetProjectDir()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSearchCode_Happy(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "main.go"), "// TODO fix this\n")

	s := &SearchCode{}
	out, err := s.Execute(context.Background(), json.RawMessage(`{"pattern":"TODO"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "TODO") {
		t.Errorf("expected match containing 'TODO', got %q", out)
	}
}

func TestSearchCode_NoMatches(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "main.go"), "package main\n")

	s := &SearchCode{}
	out, err := s.Execute(context.Background(), json.RawMessage(`{"pattern":"ZZZNOMATCH"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "no matches found" {
		t.Errorf("expected 'no matches found', got %q", out)
	}
}

func TestSearchCode_InvalidRegex(t *testing.T) {
	s := &SearchCode{}
	_, err := s.Execute(context.Background(), json.RawMessage(`{"pattern":"[invalid"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("expected 'invalid regex' error, got %v", err)
	}
}

func TestSearchCode_SkipDirs(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "node_modules", "lib.js"), "// TODO hidden\n")

	s := &SearchCode{}
	out, err := s.Execute(context.Background(), json.RawMessage(`{"pattern":"TODO"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "no matches found" {
		t.Errorf("expected 'no matches found', got %q", out)
	}
}

func TestSearchCode_BinarySkip(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "bin.dat"), "TODO\x00binary\n")

	s := &SearchCode{}
	out, err := s.Execute(context.Background(), json.RawMessage(`{"pattern":"TODO"}`))
	if err != nil {
		t.Fatal(err)
	}
	if out != "no matches found" {
		t.Errorf("expected 'no matches found', got %q", out)
	}
}

func TestSearchCode_IncludeFilter(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	writeFile(t, filepath.Join(dir, "main.go"), "// TODO go\n")
	writeFile(t, filepath.Join(dir, "notes.txt"), "TODO txt\n")

	s := &SearchCode{}
	out, err := s.Execute(context.Background(), json.RawMessage(`{"pattern":"TODO","include":"*.go"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go in output, got %q", out)
	}
	if strings.Contains(out, "notes.txt") {
		t.Errorf("expected notes.txt excluded, got %q", out)
	}
}

func TestSearchCode_MaxResults(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString("MATCH line\n")
	}
	writeFile(t, filepath.Join(dir, "big.go"), b.String())

	s := &SearchCode{}
	out, err := s.Execute(context.Background(), json.RawMessage(`{"pattern":"MATCH"}`))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 50 {
		t.Errorf("expected at most 50 matches, got %d", len(lines))
	}
}
