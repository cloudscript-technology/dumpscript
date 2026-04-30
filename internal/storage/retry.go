package storage

import (
	"context"
	"log/slog"
	"time"
)

// RetryConfig controls retry behavior for the Retrying decorator.
type RetryConfig struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// DefaultRetryConfig mirrors the bash script: 3 attempts, 5s initial → 5m cap.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:    3,
		InitialBackoff: 5 * time.Second,
		MaxBackoff:     5 * time.Minute,
	}
}

// Retrying is a Decorator around Storage that adds exponential-backoff retry.
// A caller-supplied onRetry hook (e.g. IRSA credential refresh) runs between attempts.
type Retrying struct {
	inner   Storage
	cfg     RetryConfig
	log     *slog.Logger
	onRetry func(attempt int)
}

func NewRetrying(inner Storage, cfg RetryConfig, log *slog.Logger, onRetry func(int)) *Retrying {
	return &Retrying{inner: inner, cfg: cfg, log: log, onRetry: onRetry}
}

func (r *Retrying) do(ctx context.Context, op string, fn func() error) error {
	var last error
	for attempt := 1; attempt <= r.cfg.MaxAttempts; attempt++ {
		if err := fn(); err == nil {
			return nil
		} else {
			last = err
		}
		if attempt == r.cfg.MaxAttempts {
			break
		}
		backoff := r.cfg.InitialBackoff << (attempt - 1)
		if backoff > r.cfg.MaxBackoff {
			backoff = r.cfg.MaxBackoff
		}
		r.log.Warn("storage op failed; will retry",
			"op", op, "attempt", attempt, "backoff", backoff, "err", last)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if r.onRetry != nil {
			r.onRetry(attempt)
		}
	}
	return last
}

func (r *Retrying) Upload(ctx context.Context, local, key string) error {
	return r.do(ctx, "upload", func() error { return r.inner.Upload(ctx, local, key) })
}

func (r *Retrying) UploadBytes(ctx context.Context, data []byte, key string) error {
	return r.do(ctx, "upload_bytes", func() error { return r.inner.UploadBytes(ctx, data, key) })
}

func (r *Retrying) Download(ctx context.Context, key, local string) error {
	return r.do(ctx, "download", func() error { return r.inner.Download(ctx, key, local) })
}

func (r *Retrying) Exists(ctx context.Context, key string) (bool, error) {
	var found bool
	err := r.do(ctx, "exists", func() error {
		ok, e := r.inner.Exists(ctx, key)
		found = ok
		return e
	})
	return found, err
}

func (r *Retrying) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	err := r.do(ctx, "list", func() error {
		o, e := r.inner.List(ctx, prefix)
		out = o
		return e
	})
	return out, err
}

func (r *Retrying) Delete(ctx context.Context, key string) error {
	return r.do(ctx, "delete", func() error { return r.inner.Delete(ctx, key) })
}

func (r *Retrying) DisplayPath(key string) string { return r.inner.DisplayPath(key) }
