package dumper

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// stubDumper returns a controlled sequence of (Artifact, error) pairs and
// counts how many times Dump was called.
type stubDumper struct {
	results []struct {
		art *Artifact
		err error
	}
	calls int
}

func (s *stubDumper) Dump(ctx context.Context) (*Artifact, error) {
	r := s.results[s.calls%len(s.results)]
	s.calls++
	return r.art, r.err
}

func TestRetrying_SucceedsOnFirstAttempt(t *testing.T) {
	want := &Artifact{Path: "/tmp/x.gz"}
	s := &stubDumper{results: []struct {
		art *Artifact
		err error
	}{{art: want, err: nil}}}
	r := NewRetrying(s, RetryConfig{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	got, err := r.Dump(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("artifact = %v, want %v", got, want)
	}
	if s.calls != 1 {
		t.Fatalf("calls = %d, want 1", s.calls)
	}
}

func TestRetrying_RetriesUntilSuccess(t *testing.T) {
	want := &Artifact{Path: "/tmp/x.gz"}
	s := &stubDumper{results: []struct {
		art *Artifact
		err error
	}{
		{nil, errors.New("transient 1")},
		{nil, errors.New("transient 2")},
		{want, nil},
	}}
	r := NewRetrying(s, RetryConfig{MaxAttempts: 3, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	got, err := r.Dump(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("artifact mismatch")
	}
	if s.calls != 3 {
		t.Fatalf("calls = %d, want 3", s.calls)
	}
}

func TestRetrying_GivesUpAfterMaxAttempts(t *testing.T) {
	final := errors.New("persistent failure")
	s := &stubDumper{results: []struct {
		art *Artifact
		err error
	}{{nil, final}}}
	r := NewRetrying(s, RetryConfig{MaxAttempts: 2, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := r.Dump(context.Background())
	if !errors.Is(err, final) {
		t.Fatalf("err = %v, want last attempt error", err)
	}
	if s.calls != 2 {
		t.Fatalf("calls = %d, want 2", s.calls)
	}
}

func TestRetrying_DoesNotRetryOnContextCancel(t *testing.T) {
	s := &stubDumper{results: []struct {
		art *Artifact
		err error
	}{{nil, context.Canceled}}}
	r := NewRetrying(s, RetryConfig{MaxAttempts: 5, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := r.Dump(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if s.calls != 1 {
		t.Fatalf("expected single attempt on context cancellation, got %d", s.calls)
	}
}

func TestRetrying_AttemptsClampedToOne(t *testing.T) {
	want := &Artifact{Path: "/tmp/x.gz"}
	s := &stubDumper{results: []struct {
		art *Artifact
		err error
	}{{want, nil}}}
	r := NewRetrying(s, RetryConfig{MaxAttempts: 0, InitialBackoff: time.Millisecond, MaxBackoff: time.Millisecond}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := r.Dump(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.calls != 1 {
		t.Fatalf("calls = %d, want 1", s.calls)
	}
}
