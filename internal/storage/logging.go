package storage

import (
	"context"
	"log/slog"
	"time"
)

// Logging is a Decorator around Storage that emits structured logs for every call.
type Logging struct {
	inner Storage
	log   *slog.Logger
}

func NewLogging(inner Storage, log *slog.Logger) *Logging {
	return &Logging{inner: inner, log: log}
}

func (l *Logging) Upload(ctx context.Context, local, key string) error {
	start := time.Now()
	l.log.Info("storage upload start", "local", local, "key", key)
	err := l.inner.Upload(ctx, local, key)
	if err != nil {
		l.log.Error("storage upload failed", "key", key, "err", err, "elapsed", time.Since(start))
	} else {
		l.log.Info("storage upload ok", "key", key, "elapsed", time.Since(start))
	}
	return err
}

func (l *Logging) UploadBytes(ctx context.Context, data []byte, key string) error {
	err := l.inner.UploadBytes(ctx, data, key)
	if err != nil {
		l.log.Error("storage upload_bytes failed", "key", key, "err", err)
	} else {
		l.log.Debug("storage upload_bytes ok", "key", key, "size", len(data))
	}
	return err
}

func (l *Logging) Exists(ctx context.Context, key string) (bool, error) {
	ok, err := l.inner.Exists(ctx, key)
	if err != nil {
		l.log.Error("storage exists failed", "key", key, "err", err)
	} else {
		l.log.Debug("storage exists", "key", key, "found", ok)
	}
	return ok, err
}

func (l *Logging) Download(ctx context.Context, key, local string) error {
	start := time.Now()
	l.log.Info("storage download start", "key", key, "local", local)
	err := l.inner.Download(ctx, key, local)
	if err != nil {
		l.log.Error("storage download failed", "key", key, "err", err)
	} else {
		l.log.Info("storage download ok", "key", key, "elapsed", time.Since(start))
	}
	return err
}

func (l *Logging) List(ctx context.Context, prefix string) ([]Object, error) {
	objs, err := l.inner.List(ctx, prefix)
	if err != nil {
		l.log.Error("storage list failed", "prefix", prefix, "err", err)
	} else {
		l.log.Debug("storage list ok", "prefix", prefix, "count", len(objs))
	}
	return objs, err
}

func (l *Logging) Delete(ctx context.Context, key string) error {
	err := l.inner.Delete(ctx, key)
	if err != nil {
		l.log.Error("storage delete failed", "key", key, "err", err)
	} else {
		l.log.Info("storage delete ok", "key", key)
	}
	return err
}

func (l *Logging) DisplayPath(key string) string { return l.inner.DisplayPath(key) }
