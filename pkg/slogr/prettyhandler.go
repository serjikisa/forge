// prettyhandler.go provides a colorized, human-friendly slog handler for development.
package slogr

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
)

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	yellow = "\033[33m"
	green  = "\033[32m"
	cyan   = "\033[36m"
)

// PrettyHandler prints colorized, human-friendly log lines.
type PrettyHandler struct {
	level slog.Level
}

func colorLevel(level slog.Level) string {
	switch level {
	case slog.LevelError:
		return red + level.String() + reset
	case slog.LevelWarn:
		return yellow + level.String() + reset
	case slog.LevelInfo:
		return green + level.String() + reset
	case slog.LevelDebug:
		return cyan + level.String() + reset
	default:
		return level.String()
	}
}

func (h *PrettyHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *PrettyHandler) Handle(_ context.Context, r slog.Record) error {
	ts := r.Time.Format("2006-01-02 15:04:05")
	level := colorLevel(r.Level)

	var attrs string
	r.Attrs(func(a slog.Attr) bool {
		attrs += fmt.Sprintf(" %s=%v", a.Key, a.Value)
		return true
	})

	var source string
	pc := make([]uintptr, 1)
	n := runtime.Callers(6, pc)
	if n > 0 {
		frames := runtime.CallersFrames(pc)
		if frame, _ := frames.Next(); frame.File != "" {
			parts := strings.Split(frame.File, "/")
			if len(parts) >= 2 {
				source = fmt.Sprintf(" %s/%s:%d", parts[len(parts)-2], parts[len(parts)-1], frame.Line)
			}
		}
	}

	fmt.Fprintf(os.Stdout, "%s %s%s %s%s\n", level, ts, source, r.Message, attrs)
	return nil
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *PrettyHandler) WithGroup(name string) slog.Handler       { return h }
