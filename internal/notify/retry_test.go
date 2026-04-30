package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

type stubNotifier struct {
	results []error
	calls   int
}

func (s *stubNotifier) Notify(ctx context.Context, _ Event) error {
	r := s.results[s.calls%len(s.results)]
	s.calls++
	return r
}

func TestRetrying_SucceedsAfterRetry(t *testing.T) {
	s := &stubNotifier{results: []error{errors.New("503"), errors.New("503"), nil}}
	r := NewRetrying(s, RetryConfig{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	if err := r.Notify(context.Background(), Event{}); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if s.calls != 3 {
		t.Fatalf("calls = %d, want 3", s.calls)
	}
}

func TestRetrying_StopsOnContextCancel(t *testing.T) {
	s := &stubNotifier{results: []error{context.Canceled}}
	r := NewRetrying(s, RetryConfig{MaxAttempts: 5, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	if err := r.Notify(context.Background(), Event{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if s.calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on context cancel)", s.calls)
	}
}

func TestRetrying_ReturnsLastError(t *testing.T) {
	final := errors.New("permanent")
	s := &stubNotifier{results: []error{final}}
	r := NewRetrying(s, RetryConfig{MaxAttempts: 2, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	err := r.Notify(context.Background(), Event{})
	if !errors.Is(err, final) {
		t.Fatalf("err = %v, want %v", err, final)
	}
	if s.calls != 2 {
		t.Fatalf("calls = %d, want 2 attempts", s.calls)
	}
}
