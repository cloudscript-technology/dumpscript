package storage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// fakeStorage is a controllable Storage stub for decorator tests.
type fakeStorage struct {
	uploadCalls      int
	uploadBytesCalls int
	downloadCalls    int
	listCalls        int
	deleteCalls      int
	existsCalls      int

	failUntil int   // fail the first N calls then succeed
	upErr     error // error returned while failing
	existsMap map[string]bool
}

func (f *fakeStorage) Upload(_ context.Context, _, _ string) error {
	f.uploadCalls++
	if f.uploadCalls <= f.failUntil {
		return f.upErr
	}
	return nil
}
func (f *fakeStorage) UploadBytes(_ context.Context, _ []byte, _ string) error {
	f.uploadBytesCalls++
	if f.uploadBytesCalls <= f.failUntil {
		return f.upErr
	}
	return nil
}
func (f *fakeStorage) Download(_ context.Context, _, _ string) error {
	f.downloadCalls++
	if f.downloadCalls <= f.failUntil {
		return f.upErr
	}
	return nil
}
func (f *fakeStorage) List(_ context.Context, _ string) ([]Object, error) {
	f.listCalls++
	if f.listCalls <= f.failUntil {
		return nil, f.upErr
	}
	return []Object{{Key: "k"}}, nil
}
func (f *fakeStorage) Delete(_ context.Context, _ string) error {
	f.deleteCalls++
	if f.deleteCalls <= f.failUntil {
		return f.upErr
	}
	return nil
}
func (f *fakeStorage) Exists(_ context.Context, key string) (bool, error) {
	f.existsCalls++
	if f.existsCalls <= f.failUntil {
		return false, f.upErr
	}
	return f.existsMap[key], nil
}
func (f *fakeStorage) DisplayPath(key string) string { return "fake://" + key }

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fastRetry() RetryConfig {
	return RetryConfig{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: 5 * time.Millisecond}
}

func TestRetrying_SucceedsFirstTry(t *testing.T) {
	inner := &fakeStorage{}
	r := NewRetrying(inner, fastRetry(), quietLogger(), nil)
	if err := r.Upload(context.Background(), "l", "k"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if inner.uploadCalls != 1 {
		t.Errorf("expected 1 call, got %d", inner.uploadCalls)
	}
}

func TestRetrying_SucceedsAfterFailures(t *testing.T) {
	inner := &fakeStorage{failUntil: 2, upErr: errors.New("transient")}
	onRetryCalls := 0
	r := NewRetrying(inner, fastRetry(), quietLogger(), func(int) { onRetryCalls++ })
	if err := r.Upload(context.Background(), "l", "k"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if inner.uploadCalls != 3 {
		t.Errorf("expected 3 calls, got %d", inner.uploadCalls)
	}
	if onRetryCalls != 2 {
		t.Errorf("expected onRetry called 2 times, got %d", onRetryCalls)
	}
}

func TestRetrying_ExhaustsAttempts(t *testing.T) {
	permanent := errors.New("permanent")
	inner := &fakeStorage{failUntil: 10, upErr: permanent}
	r := NewRetrying(inner, fastRetry(), quietLogger(), nil)
	err := r.Upload(context.Background(), "l", "k")
	if !errors.Is(err, permanent) {
		t.Fatalf("expected permanent err, got %v", err)
	}
	if inner.uploadCalls != 3 {
		t.Errorf("expected 3 attempts, got %d", inner.uploadCalls)
	}
}

func TestRetrying_ContextCancel(t *testing.T) {
	inner := &fakeStorage{failUntil: 10, upErr: errors.New("keep failing")}
	r := NewRetrying(inner, RetryConfig{
		MaxAttempts: 5, InitialBackoff: 50 * time.Millisecond, MaxBackoff: 50 * time.Millisecond,
	}, quietLogger(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	err := r.Upload(ctx, "l", "k")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRetrying_DelegatesAllOps(t *testing.T) {
	inner := &fakeStorage{existsMap: map[string]bool{"existing": true}}
	r := NewRetrying(inner, fastRetry(), quietLogger(), nil)
	ctx := context.Background()

	if err := r.Upload(ctx, "l", "k"); err != nil {
		t.Fatal(err)
	}
	if err := r.UploadBytes(ctx, []byte("data"), "k"); err != nil {
		t.Fatal(err)
	}
	if err := r.Download(ctx, "k", "l"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.List(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	found, err := r.Exists(ctx, "existing")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("Exists should return true for known key")
	}
	if r.DisplayPath("x") != "fake://x" {
		t.Errorf("DisplayPath not delegated")
	}
	if inner.uploadCalls != 1 || inner.uploadBytesCalls != 1 ||
		inner.downloadCalls != 1 || inner.listCalls != 1 ||
		inner.deleteCalls != 1 || inner.existsCalls != 1 {
		t.Errorf("delegation counts wrong: %+v", inner)
	}
}

func TestRetrying_ExistsAndUploadBytes_Retry(t *testing.T) {
	inner := &fakeStorage{failUntil: 1, upErr: errors.New("transient")}
	r := NewRetrying(inner, fastRetry(), quietLogger(), nil)
	if err := r.UploadBytes(context.Background(), []byte("x"), "k"); err != nil {
		t.Errorf("UploadBytes should retry: %v", err)
	}
	if _, err := r.Exists(context.Background(), "k"); err != nil {
		t.Errorf("Exists should retry: %v", err)
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.InitialBackoff != 5*time.Second {
		t.Errorf("InitialBackoff = %v, want 5s", cfg.InitialBackoff)
	}
	if cfg.MaxBackoff != 5*time.Minute {
		t.Errorf("MaxBackoff = %v, want 5m", cfg.MaxBackoff)
	}
}
