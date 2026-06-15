package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.Info("hello world", "key", "value")

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry.Level != "info" {
		t.Errorf("expected level=info, got %s", entry.Level)
	}
	if !strings.Contains(entry.Msg, "hello") {
		t.Errorf("expected msg containing 'hello', got %s", entry.Msg)
	}
	if entry.Fields["key"] != "value" {
		t.Errorf("expected fields.key=value, got %s", entry.Fields["key"])
	}
}

func TestLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.SetLevel(LevelWarn)

	l.Debug("debug msg")
	l.Info("info msg")
	if buf.Len() > 0 {
		t.Error("expected no output for debug/info below warn level")
	}

	l.Warn("warn msg")
	if buf.Len() == 0 {
		t.Error("expected warn output")
	}
}

func TestLogger_ErrorWithErr(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	l.Error("something failed", errTest, "module", "test")

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry.Level != "error" {
		t.Errorf("expected level=error, got %s", entry.Level)
	}
	if entry.Error != "test error" {
		t.Errorf("expected error='test error', got %s", entry.Error)
	}
}

func TestLogger_Module(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)
	child := l.NewModule("panel")
	child.Info("test")

	var entry LogEntry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if entry.Module != "panel" {
		t.Errorf("expected module=panel, got %s", entry.Module)
	}
}

func TestLogger_ValidJSON(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf)

	// All levels produce valid JSON
	l.Debug("debug")
	l.Info("info")
	l.Warn("warn")
	l.Error("error", nil)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %s", i, line)
		}
	}
}

func TestLogger_LevelNames(t *testing.T) {
	tests := []struct {
		lvl Level
		want string
	}{
		{LevelDebug, "debug"},
		{LevelInfo, "info"},
		{LevelWarn, "warn"},
		{LevelError, "error"},
		{Level(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.lvl.String(); got != tt.want {
			t.Errorf("Level(%d).String() = %q, want %q", tt.lvl, got, tt.want)
		}
	}
}

// errTestSentinel is a sentinel error for testing.
var errTest = &testError{msg: "test error"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
