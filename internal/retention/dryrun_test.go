package retention

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/storage"
)

// captureStore counts Delete calls so the test can assert the cleaner
// runs in dry-run mode without actually deleting anything.
type captureStore struct {
	listObjects  []storage.Object
	deletedKeys  []string
	listErr      error
	deleteErr    error
}

func (c *captureStore) Upload(_ context.Context, _, _ string) error                  { return nil }
func (c *captureStore) UploadBytes(_ context.Context, _ []byte, _ string) error      { return nil }
func (c *captureStore) Download(_ context.Context, _, _ string) error                { return nil }
func (c *captureStore) Exists(_ context.Context, _ string) (bool, error)             { return false, nil }
func (c *captureStore) DisplayPath(k string) string                                  { return k }
func (c *captureStore) List(_ context.Context, _ string) ([]storage.Object, error) {
	return c.listObjects, c.listErr
}
func (c *captureStore) Delete(_ context.Context, key string) error {
	c.deletedKeys = append(c.deletedKeys, key)
	return c.deleteErr
}

func TestCleaner_DryRunDoesNotDelete(t *testing.T) {
	now := time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC)
	old := "p/daily/2024/01/01/dump_20240101_120000.sql.gz"
	st := &captureStore{listObjects: []storage.Object{{Key: old, Size: 1}}}

	c := New(st, slog.New(slog.NewTextHandler(io.Discard, nil))).WithDryRun(true)
	r, err := c.Run(context.Background(), "p/daily/", 30, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1 (counted but not actually deleted)", r.Deleted)
	}
	if len(st.deletedKeys) != 0 {
		t.Errorf("dry-run should not call store.Delete, got %v", st.deletedKeys)
	}
}

func TestCleaner_NonDryRunActuallyDeletes(t *testing.T) {
	now := time.Date(2026, 4, 28, 0, 0, 0, 0, time.UTC)
	old := "p/daily/2024/01/01/dump_20240101_120000.sql.gz"
	st := &captureStore{listObjects: []storage.Object{{Key: old, Size: 1}}}

	c := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := c.Run(context.Background(), "p/daily/", 30, now); err != nil {
		t.Fatal(err)
	}
	if len(st.deletedKeys) != 1 || st.deletedKeys[0] != old {
		t.Errorf("expected exactly one delete of %q, got %v", old, st.deletedKeys)
	}
}

func TestCleaner_WithDryRunReturnsCopy(t *testing.T) {
	st := &captureStore{}
	base := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	dry := base.WithDryRun(true)
	if base == dry {
		t.Fatal("WithDryRun should return a new Cleaner, not mutate in place")
	}
}
