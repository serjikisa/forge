package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile_Execute(t *testing.T) {
	ctx := context.Background()

	t.Run("happy path", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		os.WriteFile("test.txt", []byte("hello world"), 0o644)
		r := &ReadFile{}
		got, err := r.Execute(ctx, json.RawMessage(`{"path":"test.txt"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "hello world" {
			t.Errorf("got %q, want %q", got, "hello world")
		}
	})

	t.Run("binary detection", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		os.WriteFile("bin.dat", []byte("abc\x00def"), 0o644)
		r := &ReadFile{}
		got, err := r.Execute(ctx, json.RawMessage(`{"path":"bin.dat"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "binary file, not displayed" {
			t.Errorf("got %q, want %q", got, "binary file, not displayed")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		os.WriteFile("empty.txt", []byte{}, 0o644)
		r := &ReadFile{}
		got, err := r.Execute(ctx, json.RawMessage(`{"path":"empty.txt"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		r := &ReadFile{}
		_, err := r.Execute(ctx, json.RawMessage(`{"path":"nope.txt"}`))
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})

	t.Run("invalid JSON params", func(t *testing.T) {
		r := &ReadFile{}
		_, err := r.Execute(ctx, json.RawMessage(`{bad json`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestWriteFile_Execute(t *testing.T) {
	ctx := context.Background()

	t.Run("happy path", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		w := &WriteFile{}
		got, err := w.Execute(ctx, json.RawMessage(`{"path":"out.txt","content":"hello"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, "5 bytes") {
			t.Errorf("got %q, expected mention of 5 bytes", got)
		}
		data, _ := os.ReadFile("out.txt")
		if string(data) != "hello" {
			t.Errorf("file content = %q, want %q", data, "hello")
		}
	})

	t.Run("go.mod guard", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		w := &WriteFile{}
		_, err := w.Execute(ctx, json.RawMessage(`{"path":"subdir/go.mod","content":"module x"}`))
		if err == nil {
			t.Fatal("expected error for go.mod in subdirectory")
		}
		if !strings.Contains(err.Error(), "would break module resolution") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		w := &WriteFile{}
		_, err := w.Execute(ctx, json.RawMessage(`{"path":"nested/path/file.txt","content":"deep"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		data, err := os.ReadFile(filepath.Join("nested", "path", "file.txt"))
		if err != nil {
			t.Fatalf("failed to read created file: %v", err)
		}
		if string(data) != "deep" {
			t.Errorf("file content = %q, want %q", data, "deep")
		}
	})

	t.Run("invalid JSON params", func(t *testing.T) {
		w := &WriteFile{}
		_, err := w.Execute(ctx, json.RawMessage(`not json`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestListDir_Execute(t *testing.T) {
	ctx := context.Background()

	t.Run("happy path", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		os.WriteFile("a.txt", []byte("a"), 0o644)
		os.Mkdir("subdir", 0o755)

		l := &ListDir{}
		got, err := l.Execute(ctx, json.RawMessage(`{"path":"."}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, "a.txt") {
			t.Errorf("output missing a.txt: %s", got)
		}
		if !strings.Contains(got, "subdir/") {
			t.Errorf("output missing trailing / on subdir: %s", got)
		}
	})

	t.Run("empty path defaults to dot", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		os.WriteFile("marker.txt", []byte("x"), 0o644)

		l := &ListDir{}
		got, err := l.Execute(ctx, json.RawMessage(`{"path":""}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, "marker.txt") {
			t.Errorf("expected marker.txt in output: %s", got)
		}
	})

	t.Run("truncation", func(t *testing.T) {
		dir := t.TempDir()
		old, _ := os.Getwd()
		os.Chdir(dir)
		defer os.Chdir(old)

		for i := 0; i < 205; i++ {
			os.WriteFile(fmt.Sprintf("file_%03d.txt", i), []byte{}, 0o644)
		}

		l := &ListDir{}
		got, err := l.Execute(ctx, json.RawMessage(`{"path":"."}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(got, "... truncated") {
			t.Errorf("expected truncation message in output: %s", got)
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		l := &ListDir{}
		_, err := l.Execute(ctx, json.RawMessage(`{"path":"/no/such/dir"}`))
		if err == nil {
			t.Fatal("expected error for nonexistent directory")
		}
	})
}
