package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"garbage", slog.LevelInfo},
	}
	for _, tc := range tests {
		if got := ParseLevel(tc.in); got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KiB"},
		{1536, "1.5KiB"},
		{1024 * 1024, "1.0MiB"},
		{int64(3.5 * 1024 * 1024), "3.5MiB"},
		{1024 * 1024 * 1024, "1.0GiB"},
		{int64(1024) * 1024 * 1024 * 1024, "1.0TiB"},
	}
	for _, tc := range tests {
		if got := humanBytes(tc.n); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Microsecond, "500µs"},
		{50 * time.Millisecond, "50ms"},
		{3500 * time.Millisecond, "3.5s"},
		{90 * time.Second, "1m30s"},
		{125 * time.Minute, "2h05m"},
	}
	for _, tc := range tests {
		if got := humanDuration(tc.d); got != tc.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestNew_JSON_DefaultFormat(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "info", Format: "json", Writer: &buf})
	l.Info("hello", "k", "v")
	out := buf.String()
	if !strings.Contains(out, `"msg":"hello"`) {
		t.Errorf("JSON output missing msg: %s", out)
	}
	if !strings.Contains(out, `"k":"v"`) {
		t.Errorf("JSON output missing attrs: %s", out)
	}
}

func TestNew_Console_HumanizesSize(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "info", Format: "console", Writer: &buf})
	l.Info("uploaded", "size", int64(3*1024*1024))
	out := buf.String()
	if !strings.Contains(out, "3.0MiB") {
		t.Errorf("console output should humanize size, got: %s", out)
	}
}

func TestNew_Console_HumanizesElapsed(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "info", Format: "console", Writer: &buf})
	l.Info("phase done", "elapsed", 3500*time.Millisecond)
	out := buf.String()
	if !strings.Contains(out, "3.5s") {
		t.Errorf("console output should humanize elapsed, got: %s", out)
	}
}

func TestNew_LevelFilter(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "warn", Format: "json", Writer: &buf})
	l.Debug("d")
	l.Info("i")
	l.Warn("w")
	out := buf.String()
	if strings.Contains(out, `"msg":"d"`) || strings.Contains(out, `"msg":"i"`) {
		t.Errorf("debug/info should be filtered, got: %s", out)
	}
	if !strings.Contains(out, `"msg":"w"`) {
		t.Errorf("warn should have been logged: %s", out)
	}
}
