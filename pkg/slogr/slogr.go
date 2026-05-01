// Package slogr provides structured logging with pretty, text, and JSON modes.
package slogr

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"
)

type LoggerType = string
type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

const (
	TypeText   LoggerType = "TEXT"
	TypeJSON   LoggerType = "JSON"
	TypePretty LoggerType = "PRETTY"
)

// Setup configures logging from string level ("debug","info","warn","error")
// and format ("text","json","pretty").
func Setup(level, format string) {
	var lvl Level
	switch level {
	case "debug":
		lvl = LevelDebug
	case "warn":
		lvl = LevelWarn
	case "error":
		lvl = LevelError
	default:
		lvl = LevelInfo
	}
	var logType LoggerType
	switch format {
	case "json":
		logType = TypeJSON
	case "pretty":
		logType = TypePretty
	default:
		logType = TypeText
	}
	Init(lvl, logType)
}

// Init configures the default slog logger with typed parameters.
// Must be called explicitly — this is not Go's auto-called init().
func Init(level Level, loggerType LoggerType) {
	var handler slog.Handler
	switch loggerType {
	case TypeJSON:
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
			Level:     level,
		})
	case TypePretty:
		handler = &PrettyHandler{level: level}
	default:
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			AddSource: true,
			Level:     level,
		})
	}
	slog.SetDefault(slog.New(handler))
}

// log emits a record with the correct caller PC (skipping this package's frames).
func log(level slog.Level, msg string, args ...any) {
	l := slog.Default()
	if !l.Enabled(context.Background(), level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	l.Handler().Handle(context.Background(), r)
}

func Info(msg string, args ...any)  { log(slog.LevelInfo, msg, args...) }
func Debug(msg string, args ...any) { log(slog.LevelDebug, msg, args...) }
func Warn(msg string, args ...any)  { log(slog.LevelWarn, msg, args...) }
func Error(msg string, args ...any) { log(slog.LevelError, msg, args...) }

func Infof(format string, a ...any)  { log(slog.LevelInfo, fmt.Sprintf(format, a...)) }
func Debugf(format string, a ...any) { log(slog.LevelDebug, fmt.Sprintf(format, a...)) }
func Warnf(format string, a ...any)  { log(slog.LevelWarn, fmt.Sprintf(format, a...)) }
func Errorf(format string, a ...any) { log(slog.LevelError, fmt.Sprintf(format, a...)) }

func Fatal(msg string, args ...any) {
	log(slog.LevelError, msg, args...)
	os.Exit(1)
}

func Fatalf(format string, a ...any) {
	log(slog.LevelError, fmt.Sprintf(format, a...))
	os.Exit(1)
}
