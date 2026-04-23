package storage

import (
	"context"
	"errors"
	"testing"
)

func TestLogging_DelegatesAndPassesErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("success path", func(t *testing.T) {
		inner := &fakeStorage{}
		l := NewLogging(inner, quietLogger())
		if err := l.Upload(ctx, "local", "key"); err != nil {
			t.Fatalf("Upload: %v", err)
		}
		if err := l.Download(ctx, "key", "local"); err != nil {
			t.Fatalf("Download: %v", err)
		}
		objs, err := l.List(ctx, "prefix")
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(objs) != 1 {
			t.Errorf("List objs = %d, want 1", len(objs))
		}
		if err := l.Delete(ctx, "key"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if l.DisplayPath("k") != "fake://k" {
			t.Errorf("DisplayPath not delegated")
		}
		if inner.uploadCalls != 1 || inner.downloadCalls != 1 || inner.listCalls != 1 || inner.deleteCalls != 1 {
			t.Errorf("inner calls = %+v", inner)
		}
	})

	t.Run("propagates errors", func(t *testing.T) {
		wantErr := errors.New("boom")
		inner := &fakeStorage{failUntil: 100, upErr: wantErr}
		l := NewLogging(inner, quietLogger())

		if err := l.Upload(ctx, "l", "k"); !errors.Is(err, wantErr) {
			t.Errorf("Upload err = %v", err)
		}
		if err := l.UploadBytes(ctx, []byte("x"), "k"); !errors.Is(err, wantErr) {
			t.Errorf("UploadBytes err = %v", err)
		}
		if err := l.Download(ctx, "k", "l"); !errors.Is(err, wantErr) {
			t.Errorf("Download err = %v", err)
		}
		if _, err := l.List(ctx, "p"); !errors.Is(err, wantErr) {
			t.Errorf("List err = %v", err)
		}
		if err := l.Delete(ctx, "k"); !errors.Is(err, wantErr) {
			t.Errorf("Delete err = %v", err)
		}
		if _, err := l.Exists(ctx, "k"); !errors.Is(err, wantErr) {
			t.Errorf("Exists err = %v", err)
		}
	})

	t.Run("exists and uploadbytes success", func(t *testing.T) {
		inner := &fakeStorage{existsMap: map[string]bool{"present": true}}
		l := NewLogging(inner, quietLogger())
		if err := l.UploadBytes(ctx, []byte("x"), "k"); err != nil {
			t.Errorf("UploadBytes: %v", err)
		}
		ok, err := l.Exists(ctx, "present")
		if err != nil {
			t.Errorf("Exists: %v", err)
		}
		if !ok {
			t.Error("expected Exists true for seeded key")
		}
	})
}
