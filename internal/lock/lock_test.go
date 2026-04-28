package lock

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/storage"
)

// stubStorage implements storage.Storage for lock tests.
type stubStorage struct {
	existsMap    map[string]bool
	existsErr    error
	uploadedKey  string
	uploadedData []byte
	uploadErr    error
	deleted      []string
	deleteErr    error
}

func (s *stubStorage) Upload(_ context.Context, _, _ string) error { return nil }
func (s *stubStorage) UploadBytes(_ context.Context, data []byte, key string) error {
	if s.uploadErr != nil {
		return s.uploadErr
	}
	s.uploadedKey = key
	s.uploadedData = append([]byte(nil), data...)
	if s.existsMap == nil {
		s.existsMap = map[string]bool{}
	}
	s.existsMap[key] = true
	return nil
}
func (s *stubStorage) Download(_ context.Context, key, localPath string) error {
	data := s.uploadedData
	if data == nil {
		// Fall back to existsMap-only entries with empty body. AcquireWithGrace
		// will treat unparseable JSON as stale, which the tests rely on.
		data = []byte{}
	}
	return os.WriteFile(localPath, data, 0o600)
}
func (s *stubStorage) List(_ context.Context, _ string) ([]storage.Object, error) {
	return nil, nil
}
func (s *stubStorage) Delete(_ context.Context, k string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = append(s.deleted, k)
	if s.existsMap != nil {
		delete(s.existsMap, k)
	}
	return nil
}
func (s *stubStorage) Exists(_ context.Context, key string) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	return s.existsMap[key], nil
}
func (s *stubStorage) DisplayPath(k string) string { return "stub://" + k }

func TestAcquire_Writes_WhenAbsent(t *testing.T) {
	st := &stubStorage{}
	info := NewInfo("exec-123")

	if err := Acquire(context.Background(), st, "lk", info); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if st.uploadedKey != "lk" {
		t.Errorf("uploadedKey = %q", st.uploadedKey)
	}
	var got Info
	if err := json.Unmarshal(st.uploadedData, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ExecutionID != "exec-123" {
		t.Errorf("ExecutionID = %q", got.ExecutionID)
	}
	if got.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", got.PID, os.Getpid())
	}
	if got.StartedAt.IsZero() {
		t.Error("StartedAt not set")
	}
}

func TestAcquire_ReturnsErrLocked_WhenExists(t *testing.T) {
	st := &stubStorage{existsMap: map[string]bool{"lk": true}}
	err := Acquire(context.Background(), st, "lk", NewInfo("x"))
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
	if st.uploadedKey != "" {
		t.Errorf("should not upload when locked, got %q", st.uploadedKey)
	}
}

func TestAcquire_ExistsError(t *testing.T) {
	wantErr := errors.New("exists failed")
	st := &stubStorage{existsErr: wantErr}
	err := Acquire(context.Background(), st, "lk", NewInfo("x"))
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestAcquire_UploadError(t *testing.T) {
	wantErr := errors.New("upload failed")
	st := &stubStorage{uploadErr: wantErr}
	err := Acquire(context.Background(), st, "lk", NewInfo("x"))
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestRelease_DeletesLock(t *testing.T) {
	st := &stubStorage{existsMap: map[string]bool{"lk": true}}
	if err := Release(context.Background(), st, "lk"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if len(st.deleted) != 1 || st.deleted[0] != "lk" {
		t.Errorf("deleted = %v", st.deleted)
	}
	if st.existsMap["lk"] {
		t.Error("lock still marked exists")
	}
}

func TestRelease_PropagatesDeleteError(t *testing.T) {
	wantErr := errors.New("delete failed")
	st := &stubStorage{deleteErr: wantErr}
	err := Release(context.Background(), st, "lk")
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestNewInfo_PopulatesFields(t *testing.T) {
	before := time.Now().UTC()
	i := NewInfo("abc")
	after := time.Now().UTC()

	if i.ExecutionID != "abc" {
		t.Errorf("ExecutionID = %q", i.ExecutionID)
	}
	if i.PID != os.Getpid() {
		t.Errorf("PID = %d", i.PID)
	}
	if i.Hostname == "" {
		t.Error("Hostname empty")
	}
	if i.StartedAt.Before(before) || i.StartedAt.After(after.Add(time.Second)) {
		t.Errorf("StartedAt out of range: %v", i.StartedAt)
	}
}

func TestNewExecutionID_Format(t *testing.T) {
	id, err := NewExecutionID()
	if err != nil {
		t.Fatalf("NewExecutionID: %v", err)
	}
	re := regexp.MustCompile(`^[0-9a-f]{16}$`)
	if !re.MatchString(id) {
		t.Errorf("id %q does not match 16-hex format", id)
	}
}

func TestAcquireWithGrace_TakesOverStaleLock(t *testing.T) {
	st := &stubStorage{existsMap: map[string]bool{"lk": true}}
	stale := Info{ExecutionID: "old", StartedAt: time.Now().Add(-48 * time.Hour)}
	st.uploadedData, _ = json.Marshal(stale)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := AcquireWithGrace(context.Background(), st, "lk", NewInfo("new"), 24*time.Hour, logger); err != nil {
		t.Fatalf("expected stale takeover to succeed, got %v", err)
	}
	// After takeover, uploadedData should be the new info.
	var got Info
	if err := json.Unmarshal(st.uploadedData, &got); err != nil {
		t.Fatal(err)
	}
	if got.ExecutionID != "new" {
		t.Errorf("ExecutionID = %q, want new", got.ExecutionID)
	}
}

func TestAcquireWithGrace_FreshLockReturnsErrLocked(t *testing.T) {
	st := &stubStorage{existsMap: map[string]bool{"lk": true}}
	fresh := Info{ExecutionID: "running", StartedAt: time.Now().Add(-5 * time.Minute)}
	st.uploadedData, _ = json.Marshal(fresh)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := AcquireWithGrace(context.Background(), st, "lk", NewInfo("new"), 24*time.Hour, logger)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked for fresh lock", err)
	}
}

func TestAcquireWithGrace_GraceZeroIsStrictMode(t *testing.T) {
	st := &stubStorage{existsMap: map[string]bool{"lk": true}}
	stale := Info{ExecutionID: "old", StartedAt: time.Now().Add(-72 * time.Hour)}
	st.uploadedData, _ = json.Marshal(stale)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := AcquireWithGrace(context.Background(), st, "lk", NewInfo("new"), 0, logger)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked when grace=0", err)
	}
}

func TestAcquireWithGrace_MalformedLockTreatedAsStale(t *testing.T) {
	st := &stubStorage{existsMap: map[string]bool{"lk": true}}
	st.uploadedData = []byte("not-json{{{")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := AcquireWithGrace(context.Background(), st, "lk", NewInfo("new"), 24*time.Hour, logger)
	if err != nil {
		t.Fatalf("malformed lock should be taken over, got err=%v", err)
	}
	// Verify the new lock content was written.
	var got Info
	if jsonErr := json.Unmarshal(st.uploadedData, &got); jsonErr != nil {
		t.Fatal(jsonErr)
	}
	if got.ExecutionID != "new" {
		t.Errorf("ExecutionID = %q, want new", got.ExecutionID)
	}
}

func TestNewExecutionID_Unique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := NewExecutionID()
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Errorf("duplicate id: %s", id)
		}
		seen[id] = true
	}
}
