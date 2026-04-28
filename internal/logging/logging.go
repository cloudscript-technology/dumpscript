// Package logging builds the project's *slog.Logger from config.
//
// Two formats are supported:
//
//   - json (default) — slog.JSONHandler, stable fields for log aggregators.
//   - console        — tint handler with ANSI colors, HH:MM:SS timestamps and
//                      human-readable sizes/durations for local development.
//
// LOG_LEVEL selects verbosity (debug | info | warn | error).
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/lmittmann/tint"
)

// Config is self-contained so this package does not import internal/config.
type Config struct {
	Level  string // debug | info | warn | error
	Format string // json | console
	Writer io.Writer
}

// New returns a configured *slog.Logger.
func New(cfg Config) *slog.Logger {
	w := cfg.Writer
	if w == nil {
		w = os.Stderr
	}
	lvl := ParseLevel(cfg.Level)

	switch strings.ToLower(cfg.Format) {
	case "console":
		return slog.New(tint.NewHandler(w, &tint.Options{
			Level:       lvl,
			TimeFormat:  time.TimeOnly,
			ReplaceAttr: chainAttrs(redactSensitive, humanize),
		}))
	default:
		return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level:       lvl,
			ReplaceAttr: redactJSONAttr,
		}))
	}
}

// chainAttrs composes ReplaceAttr functions left-to-right.
func chainAttrs(fns ...func([]string, slog.Attr) slog.Attr) func([]string, slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		for _, fn := range fns {
			a = fn(groups, a)
		}
		return a
	}
}

// redactSensitive masks any attr whose key looks sensitive (password, secret,
// token, credential, *_key, api_key) replacing the value with [REDACTED].
// Applied to both JSON and console outputs so a stray Info("...", "password",
// pwd) call cannot leak credentials regardless of format.
func redactSensitive(_ []string, a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) && a.Value.Kind() != slog.KindGroup {
		return slog.Attr{Key: a.Key, Value: slog.StringValue("[REDACTED]")}
	}
	return a
}

// redactJSONAttr is the JSON handler's ReplaceAttr — slog.HandlerOptions only
// accepts a single ReplaceAttr, so we wrap.
func redactJSONAttr(groups []string, a slog.Attr) slog.Attr {
	return redactSensitive(groups, a)
}

// isSensitiveKey returns true when the key matches a known credential-bearing
// substring. Case-insensitive. Errs on the side of redacting — false positives
// (a "secret_count" attr) are preferable to silently leaking a token.
func isSensitiveKey(k string) bool {
	lk := strings.ToLower(k)
	for _, needle := range []string{
		"password", "passwd", "secret", "token", "credential", "apikey",
	} {
		if strings.Contains(lk, needle) {
			return true
		}
	}
	if strings.HasSuffix(lk, "_key") || lk == "key" {
		return true
	}
	return false
}

// ParseLevel maps a string to slog.Level, defaulting to Info.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// humanize pretty-prints `size` / `*_bytes` / `elapsed` / `*_duration` attrs
// so console output reads like "size=4.2MiB elapsed=3.1s".
func humanize(_ []string, a slog.Attr) slog.Attr {
	switch {
	case a.Key == "size", strings.HasSuffix(a.Key, "_bytes"):
		if n, ok := toInt64(a.Value); ok {
			a.Value = slog.StringValue(humanBytes(n))
		}
	case a.Key == "elapsed", strings.HasSuffix(a.Key, "_duration"):
		if a.Value.Kind() == slog.KindDuration {
			a.Value = slog.StringValue(humanDuration(a.Value.Duration()))
		}
	}
	return a
}

func toInt64(v slog.Value) (int64, bool) {
	switch v.Kind() {
	case slog.KindInt64:
		return v.Int64(), true
	case slog.KindUint64:
		return int64(v.Uint64()), true
	case slog.KindFloat64:
		return int64(v.Float64()), true
	}
	return 0, false
}

// humanBytes renders binary units (KiB/MiB/GiB/TiB/PiB).
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// humanDuration renders short forms: 850ms / 3.2s / 1m04s / 1h03m.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return d.String()
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Hour:
		m := int(d / time.Minute)
		s := int((d - time.Duration(m)*time.Minute) / time.Second)
		return fmt.Sprintf("%dm%02ds", m, s)
	default:
		h := int(d / time.Hour)
		m := int((d - time.Duration(h)*time.Hour) / time.Minute)
		return fmt.Sprintf("%dh%02dm", h, m)
	}
}
