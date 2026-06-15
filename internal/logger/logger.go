// Package logger provides structured JSON logging for OpenFusion.
//
// All log entries are single-line JSON, designed for production log ingestion
// (ELK, Datadog, Loki, etc.). Each entry has:
//   - Level: error / warn / info / debug
//   - Time: ISO 8601 timestamp
//   - Msg: human-readable summary
//   - Extra fields: context-specific key=value pairs
//
// Usage:
//
//	logger.Info("server started", "addr", ":8080", "presets", 7)
//	logger.Error("panel timeout", "model", "gpt-4", "duration_ms", 30000)
package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// Level represents the log severity.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// LogEntry is the structured JSON payload.
type LogEntry struct {
	Level   string            `json:"level"`
	Time    string            `json:"time"`
	Msg     string            `json:"msg"`
	Module  string            `json:"module,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
	Error   string            `json:"error,omitempty"`
	TraceID string            `json:"trace_id,omitempty"`
}

// Logger is a structured JSON logger.
type Logger struct {
	w     io.Writer
	lvl   atomic.Int64
	module string
}

// New creates a Logger writing to w.
// If w is nil, writes to os.Stderr.
func New(w io.Writer) *Logger {
	if w == nil {
		w = os.Stderr
	}
	l := &Logger{w: w, module: "openfusion"}
	l.lvl.Store(int64(LevelInfo))
	return l
}

// NewModule creates a child logger scoped to a module name.
func (l *Logger) NewModule(name string) *Logger {
	return &Logger{
		w:      l.w,
		module: name,
	}
}

// SetLevel controls the minimum log level.
func (l *Logger) SetLevel(lvl Level) {
	l.lvl.Store(int64(lvl))
}

// Debug logs at debug level.
func (l *Logger) Debug(msg string, kv ...string) {
	if LevelDebug < l.level() {
		return
	}
	l.log(LevelDebug, msg, nil, kv...)
}

// Info logs at info level.
func (l *Logger) Info(msg string, kv ...string) {
	if LevelInfo < l.level() {
		return
	}
	l.log(LevelInfo, msg, nil, kv...)
}

// Warn logs at warn level.
func (l *Logger) Warn(msg string, kv ...string) {
	if LevelWarn < l.level() {
		return
	}
	l.log(LevelWarn, msg, nil, kv...)
}

// Error logs at error level with an optional wrapped error.
func (l *Logger) Error(msg string, err error, kv ...string) {
	if LevelError < l.level() {
		return
	}
	l.log(LevelError, msg, err, kv...)
}

// Fatal logs at error level then exits with code 1.
func (l *Logger) Fatal(msg string, err error, kv ...string) {
	l.log(LevelError, msg, err, kv...)
	os.Exit(1)
}

// log writes a single structured JSON line.
func (l *Logger) log(lvl Level, msg string, err error, kv ...string) {
	entry := LogEntry{
		Level:  lvl.String(),
		Time:   time.Now().UTC().Format(time.RFC3339Nano),
		Msg:    msg,
		Module: l.module,
	}

	if err != nil {
		entry.Error = err.Error()
	}

	// Set fields from key=value pairs
	if len(kv) > 0 {
		entry.Fields = make(map[string]string, len(kv)/2)
		for i := 0; i < len(kv)-1; i += 2 {
			entry.Fields[kv[i]] = fmt.Sprintf("%v", kv[i+1])
		}
	}

	data, _ := json.Marshal(entry)
	fmt.Fprintln(l.w, string(data))
}

func (l *Logger) level() Level {
	return Level(l.lvl.Load())
}

// ---------------------------------------------------------------------------
// Global default logger (backward compat for startup logs)
// ---------------------------------------------------------------------------

// LogLevel controls the global logger level at startup.
var LogLevel = "info"

// defaultLogger is the package-level logger used by package-level functions.
var defaultLogger = New(os.Stderr)

// InitFromEnv reads OPENFUSION_LOG_LEVEL and configures the default logger.
func InitFromEnv() {
	switch strings.ToLower(os.Getenv("OPENFUSION_LOG_LEVEL")) {
	case "debug":
		defaultLogger.SetLevel(LevelDebug)
	case "warn", "warning":
		defaultLogger.SetLevel(LevelWarn)
	case "error":
		defaultLogger.SetLevel(LevelError)
	default:
		defaultLogger.SetLevel(LevelInfo)
	}
}

// Debug logs to the default logger.
func Debug(msg string, kv ...string) { defaultLogger.Debug(msg, kv...) }

// Info logs to the default logger.
func Info(msg string, kv ...string) { defaultLogger.Info(msg, kv...) }

// Warn logs to the default logger.
func Warn(msg string, kv ...string) { defaultLogger.Warn(msg, kv...) }

// Error logs to the default logger.
func Error(msg string, err error, kv ...string) { defaultLogger.Error(msg, err, kv...) }

// Fatal logs to the default logger then exits.
func Fatal(msg string, err error, kv ...string) { defaultLogger.Fatal(msg, err, kv...) }

// StopUsingStdlibLog disables the standard log package output
// by redirecting it to our structured logger.
func StopUsingStdlibLog() {
	log.SetOutput(io.Discard)
}

// ---------------------------------------------------------------------------
// Caller-skip helpers
// ---------------------------------------------------------------------------

// caller returns the file:line of the caller n frames up.
func caller(skip int) string {
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return "?:?"
	}
	// Shorten path: /home/user/go/src/github.com/.../file.go → pkg/file.go
	if idx := strings.LastIndex(file, "/internal/"); idx >= 0 {
		file = file[idx+1:]
	} else if idx := strings.LastIndex(file, "/cmd/"); idx >= 0 {
		file = file[idx+1:]
	}
	return fmt.Sprintf("%s:%d", file, line)
}
