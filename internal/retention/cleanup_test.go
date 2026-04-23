package retention

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/storage"
)

// fakeStorage implements storage.Storage for retention tests.
type fakeStorage struct {
	objects    []storage.Object
	listErr    error
	deleted    []string
	deleteFail map[string]bool // keys whose delete should fail
}

func (f *fakeStorage) Upload(_ context.Context, _, _ string) error        { return nil }
func (f *fakeStorage) UploadBytes(_ context.Context, _ []byte, _ string) error { return nil }
func (f *fakeStorage) Download(_ context.Context, _, _ string) error       { return nil }
func (f *fakeStorage) List(_ context.Context, _ string) ([]storage.Object, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.objects, nil
}
func (f *fakeStorage) Delete(_ context.Context, key string) error {
	if f.deleteFail[key] {
		return errors.New("forced delete failure")
	}
	f.deleted = append(f.deleted, key)
	return nil
}
func (f *fakeStorage) Exists(_ context.Context, _ string) (bool, error) { return false, nil }
func (f *fakeStorage) DisplayPath(k string) string                      { return "fake://" + k }

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestCleaner_Disabled(t *testing.T) {
	fs := &fakeStorage{}
	c := New(fs, quietLogger())
	r, err := c.Run(context.Background(), "p/", 0, time.Now())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Deleted != 0 || r.Kept != 0 {
		t.Errorf("expected all zeros, got %+v", r)
	}
	if len(fs.deleted) != 0 {
		t.Error("Delete should not be called")
	}
}

func TestCleaner_ListError(t *testing.T) {
	want := errors.New("list fail")
	fs := &fakeStorage{listErr: want}
	c := New(fs, quietLogger())
	_, err := c.Run(context.Background(), "p/", 7, time.Now())
	if !errors.Is(err, want) {
		t.Errorf("err = %v, want %v", err, want)
	}
}

func TestCleaner_DatePathFiltering(t *testing.T) {
	// "Now" is 2025-03-24. retention=7 → cutoff 2025-03-17.
	now := time.Date(2025, 3, 24, 0, 0, 0, 0, time.UTC)
	fs := &fakeStorage{
		objects: []storage.Object{
			{Key: "postgresql-dumps/daily/2025/03/10/dump.sql.gz"},     // old → delete
			{Key: "postgresql-dumps/daily/2025/03/16/dump.sql.gz"},     // old → delete
			{Key: "postgresql-dumps/daily/2025/03/17/dump.sql.gz"},     // == cutoff → keep
			{Key: "postgresql-dumps/daily/2025/03/20/dump.sql.gz"},     // recent → keep
			{Key: "postgresql-dumps/daily/2025/03/24/dump.sql.gz"},     // today → keep
			{Key: "postgresql-dumps/daily/2025/03/20/dump.archive.gz"}, // mongo — keep
			{Key: "postgresql-dumps/daily/2025/03/10/dump.archive"},    // old archive → delete
			{Key: "postgresql-dumps/daily/malformed-path.sql.gz"},      // no date segment → skipped
			{Key: "postgresql-dumps/daily/2025/03/10/extra.json"},      // wrong ext → skipped
		},
	}
	c := New(fs, quietLogger())
	r, err := c.Run(context.Background(), "postgresql-dumps/daily/", 7, now)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Deleted != 3 {
		t.Errorf("Deleted = %d, want 3", r.Deleted)
	}
	if r.Kept != 4 {
		t.Errorf("Kept = %d, want 4", r.Kept)
	}
	if r.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", r.Skipped)
	}
	if len(fs.deleted) != 3 {
		t.Errorf("actual deletes = %d", len(fs.deleted))
	}
}

func TestCleaner_DeleteFailureDoesNotAbort(t *testing.T) {
	now := time.Date(2025, 3, 24, 0, 0, 0, 0, time.UTC)
	fs := &fakeStorage{
		objects: []storage.Object{
			{Key: "p/daily/2025/03/10/a.sql.gz"},
			{Key: "p/daily/2025/03/11/b.sql.gz"},
		},
		deleteFail: map[string]bool{"p/daily/2025/03/10/a.sql.gz": true},
	}
	c := New(fs, quietLogger())
	r, err := c.Run(context.Background(), "p/daily/", 7, now)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", r.Deleted)
	}
}

func TestCleaner_NegativeRetention(t *testing.T) {
	fs := &fakeStorage{
		objects: []storage.Object{{Key: "p/daily/2020/01/01/old.sql.gz"}},
	}
	c := New(fs, quietLogger())
	r, err := c.Run(context.Background(), "p/", -5, time.Now())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Deleted != 0 {
		t.Errorf("should not delete with negative retention, got %d", r.Deleted)
	}
}

func TestCleaner_EmptyList(t *testing.T) {
	fs := &fakeStorage{}
	c := New(fs, quietLogger())
	r, err := c.Run(context.Background(), "p/", 7, time.Now())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if r.Deleted != 0 || r.Kept != 0 || r.Skipped != 0 {
		t.Errorf("expected all zero, got %+v", r)
	}
}
