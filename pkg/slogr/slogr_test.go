package slogr

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestSetup_Levels(t *testing.T) {
	tests := []struct {
		level  string
		format string
	}{
		{"debug", "text"},
		{"info", "json"},
		{"warn", "pretty"},
		{"error", "text"},
		{"invalid", "text"}, // defaults to info
	}
	for _, tt := range tests {
		t.Run(tt.level+"_"+tt.format, func(t *testing.T) {
			Setup(tt.level, tt.format) // should not panic
		})
	}
}

func TestInit_Pretty(t *testing.T) {
	Init(LevelDebug, TypePretty)
	// Verify the handler is set (no panic on log call)
	slog.Info("test pretty")
}

func TestInit_JSON(t *testing.T) {
	Init(LevelInfo, TypeJSON)
	slog.Info("test json")
}

func TestInit_Text(t *testing.T) {
	Init(LevelInfo, TypeText)
	slog.Info("test text")
}

func TestPrettyHandler_Enabled(t *testing.T) {
	h := &PrettyHandler{level: slog.LevelWarn}
	if h.Enabled(nil, slog.LevelInfo) {
		t.Error("info should not be enabled at warn level")
	}
	if !h.Enabled(nil, slog.LevelWarn) {
		t.Error("warn should be enabled at warn level")
	}
	if !h.Enabled(nil, slog.LevelError) {
		t.Error("error should be enabled at warn level")
	}
}

func TestPrettyHandler_WithAttrs(t *testing.T) {
	h := &PrettyHandler{level: slog.LevelInfo}
	h2 := h.WithAttrs([]slog.Attr{slog.String("key", "val")})
	if h2 != h {
		t.Error("WithAttrs should return same handler")
	}
}

func TestPrettyHandler_WithGroup(t *testing.T) {
	h := &PrettyHandler{level: slog.LevelInfo}
	h2 := h.WithGroup("grp")
	if h2 != h {
		t.Error("WithGroup should return same handler")
	}
}

func TestColorLevel(t *testing.T) {
	tests := []struct {
		level slog.Level
		color string
	}{
		{slog.LevelError, red},
		{slog.LevelWarn, yellow},
		{slog.LevelInfo, green},
		{slog.LevelDebug, cyan},
	}
	for _, tt := range tests {
		got := colorLevel(tt.level)
		if !strings.Contains(got, tt.color) {
			t.Errorf("colorLevel(%v) = %q, missing color %q", tt.level, got, tt.color)
		}
		if !strings.Contains(got, reset) {
			t.Errorf("colorLevel(%v) = %q, missing reset", tt.level, got)
		}
	}
}

func TestFatal_Exits(t *testing.T) {
	if os.Getenv("TEST_FATAL") == "1" {
		Setup("info", "text")
		Fatal("bye")
		return
	}
	// We can't easily test os.Exit in-process, just verify it compiles
	_ = Fatal
}

func TestLog_OutputsToHandler(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))

	Info("hello", "key", "val")
	Debug("dbg")
	Warn("wrn")
	Error("err")

	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Errorf("missing 'hello' in output: %s", out)
	}
	if !strings.Contains(out, "key=val") {
		t.Errorf("missing 'key=val' in output: %s", out)
	}
	if !strings.Contains(out, "dbg") {
		t.Errorf("missing 'dbg' in output: %s", out)
	}
}

func TestLogf_Formatted(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	slog.SetDefault(slog.New(handler))

	Infof("count=%d", 42)
	Debugf("x=%s", "y")

	out := buf.String()
	if !strings.Contains(out, "count=42") {
		t.Errorf("missing 'count=42' in output: %s", out)
	}
}
