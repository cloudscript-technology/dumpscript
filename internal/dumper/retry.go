package dumper

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// RetryConfig controls retry behavior for the Retrying decorator.
type RetryConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// DefaultRetryConfig mirrors the storage retry defaults: 3 attempts, 5s → 5m.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 5 * time.Second,
		MaxBackoff:     5 * time.Minute,
	}
}

// Retrying wraps an inner Dumper with exponential-backoff retry. Useful when
// the dump command is sensitive to transient network blips (e.g. mongodump
// against a cluster mid-failover, pg_dump against a host with brief packet
// loss). Context cancellation is propagated and stops retrying immediately.
type Retrying struct {
	inner Dumper
	cfg   RetryConfig
	log   *slog.Logger
}

// NewRetrying wraps inner with retry behavior. Pass cfg with MaxAttempts=1 to
// effectively disable retries while still going through this layer (so the
// pipeline doesn't need a separate code path for the no-retry case).
func NewRetrying(inner Dumper, cfg RetryConfig, log *slog.Logger) *Retrying {
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}
	return &Retrying{inner: inner, cfg: cfg, log: log}
}

// Dump runs the inner dumper, retrying transient failures with exponential
// backoff. The artifact returned is the one from the *successful* attempt —
// failed-attempt artifacts are cleaned up so the caller doesn't see partial
// dumps lingering on disk.
func (r *Retrying) Dump(ctx context.Context) (*Artifact, error) {
	var last error
	for attempt := 1; attempt <= r.cfg.MaxAttempts; attempt++ {
		art, err := r.inner.Dump(ctx)
		if err == nil {
			return art, nil
		}
		// Don't retry when the context was cancelled — the user / parent
		// timeout asked us to stop.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		last = err
		if attempt == r.cfg.MaxAttempts {
			break
		}
		backoff := r.cfg.InitialBackoff << (attempt - 1)
		if backoff > r.cfg.MaxBackoff {
			backoff = r.cfg.MaxBackoff
		}
		if r.log != nil {
			r.log.Warn("dump failed; will retry",
				"attempt", attempt, "backoff", backoff, "err", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, last
}
