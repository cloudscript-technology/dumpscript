package notify

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// RetryConfig controls retry behavior for the Retrying notifier decorator.
type RetryConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// DefaultRetryConfig — 3 attempts, 1s → 30s. Tighter than storage retries
// because notifications are best-effort; we don't want a misbehaving Slack
// webhook to delay the dump pipeline by 15 minutes.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
	}
}

// Retrying wraps any Notifier with exponential-backoff retry. Errors from the
// inner notifier are retried; context cancellation is propagated and ends the
// retry loop immediately.
//
// Notifications are *best-effort*: the pipeline ignores Notify() return values,
// so a misbehaving channel doesn't block backups. This decorator just gives
// transient failures (Slack 503, network blip) one or two more chances before
// the failure is logged.
type Retrying struct {
	inner Notifier
	cfg   RetryConfig
	log   *slog.Logger
	name  string
}

// NewRetrying wraps inner with retry. `name` shows up in retry log lines so
// operators can tell which channel is misbehaving.
func NewRetrying(inner Notifier, cfg RetryConfig, log *slog.Logger, name string) *Retrying {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}
	return &Retrying{inner: inner, cfg: cfg, log: log, name: name}
}

// Inner exposes the wrapped notifier — useful in tests that need to type-
// assert against the concrete implementation (e.g. *Slack) without leaking
// the decorator.
func (r *Retrying) Inner() Notifier { return r.inner }

func (r *Retrying) Notify(ctx context.Context, e Event) error {
	var last error
	for attempt := 1; attempt <= r.cfg.MaxAttempts; attempt++ {
		if err := r.inner.Notify(ctx, e); err == nil {
			return nil
		} else {
			last = err
		}
		if errors.Is(last, context.Canceled) || errors.Is(last, context.DeadlineExceeded) {
			return last
		}
		if attempt == r.cfg.MaxAttempts {
			break
		}
		backoff := r.cfg.InitialBackoff << (attempt - 1)
		if backoff > r.cfg.MaxBackoff {
			backoff = r.cfg.MaxBackoff
		}
		if r.log != nil {
			r.log.Warn("notifier failed; will retry",
				"channel", r.name, "attempt", attempt, "backoff", backoff, "err", last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return last
}
