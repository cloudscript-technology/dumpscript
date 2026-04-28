package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestRedactSensitive verifies that any attr whose key matches a known
// credential pattern has its value replaced with [REDACTED]. Both formats
// (json + console) get the redaction applied via ReplaceAttr.
func TestRedactSensitive(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want bool // true if value should be redacted
	}{
		{"plain password", "password", true},
		{"db password attr", "db_password", true},
		{"upper PASSWORD", "PASSWORD", true},
		{"camel apiKey", "apiKey", true},
		{"snake api_key", "api_key", true},
		{"token", "auth_token", true},
		{"secret", "client_secret", true},
		{"credential", "aws_credential", true},
		{"bare key", "key", true},
		{"trailing _key", "primary_key", true},
		{"unrelated host", "host", false},
		{"size", "size", false},
		{"db_name", "db_name", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isSensitiveKey(tc.key)
			if got != tc.want {
				t.Fatalf("isSensitiveKey(%q) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

// TestNew_JSONHandlerRedacts ensures the actual handler returned by New()
// scrubs sensitive attrs from the emitted JSON.
func TestNew_JSONHandlerRedacts(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Level: "info", Format: "json", Writer: &buf})
	log.Info("test", "password", "supersecret", "host", "db.local")

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, buf.String())
	}
	if v, _ := got["password"].(string); v != "[REDACTED]" {
		t.Fatalf("password not redacted: got %q", v)
	}
	if v, _ := got["host"].(string); v != "db.local" {
		t.Fatalf("host should not be redacted: got %q", v)
	}
}

// TestNew_ConsoleHandlerRedacts ensures the console (tint) handler also
// redacts sensitive attrs.
func TestNew_ConsoleHandlerRedacts(t *testing.T) {
	var buf bytes.Buffer
	log := New(Config{Level: "info", Format: "console", Writer: &buf})
	log.Info("ping", slog.String("token", "abcdef123"))

	out := buf.String()
	if strings.Contains(out, "abcdef123") {
		t.Fatalf("raw secret leaked in console output:\n%s", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker in console output:\n%s", out)
	}
}
