package pipeline

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/clock"
	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/dumper"
	"github.com/cloudscript-technology/dumpscript/internal/notify"
	"github.com/cloudscript-technology/dumpscript/internal/storage"
)

type mockDumper struct {
	artifact *dumper.Artifact
	err      error
	called   int
}

func (m *mockDumper) Dump(_ context.Context) (*dumper.Artifact, error) {
	m.called++
	return m.artifact, m.err
}

type mockStorage struct {
	uploadErr          error
	uploadCalls        int
	uploadedKey        string
	uploadedPath       string
	uploadBytesErr     error
	uploadedBytes      map[string][]byte
	listErr            error
	listObjects        []storage.Object
	deleted            []string
	downloadErr        error
	downloadDst        string
	existsMap          map[string]bool
	existsErr          error
}

func (m *mockStorage) Upload(_ context.Context, local, key string) error {
	m.uploadCalls++
	m.uploadedPath = local
	m.uploadedKey = key
	return m.uploadErr
}
func (m *mockStorage) UploadBytes(_ context.Context, data []byte, key string) error {
	if m.uploadBytesErr != nil {
		return m.uploadBytesErr
	}
	if m.uploadedBytes == nil {
		m.uploadedBytes = map[string][]byte{}
	}
	m.uploadedBytes[key] = data
	if m.existsMap == nil {
		m.existsMap = map[string]bool{}
	}
	m.existsMap[key] = true
	return nil
}
func (m *mockStorage) Download(_ context.Context, _, local string) error {
	m.downloadDst = local
	return m.downloadErr
}
func (m *mockStorage) List(_ context.Context, _ string) ([]storage.Object, error) {
	return m.listObjects, m.listErr
}
func (m *mockStorage) Delete(_ context.Context, k string) error {
	m.deleted = append(m.deleted, k)
	if m.existsMap != nil {
		delete(m.existsMap, k)
	}
	return nil
}
func (m *mockStorage) Exists(_ context.Context, key string) (bool, error) {
	if m.existsErr != nil {
		return false, m.existsErr
	}
	return m.existsMap[key], nil
}
func (m *mockStorage) DisplayPath(k string) string { return "s3://bucket/" + k }

type mockNotifier struct{ events []notify.Event }

func (m *mockNotifier) Notify(_ context.Context, e notify.Event) error {
	m.events = append(m.events, e)
	return nil
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func writeGzipFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "dump_20250324_120000.sql.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gw := gzip.NewWriter(f)
	_, _ = gw.Write([]byte("SELECT 1;"))
	_ = gw.Close()
	_ = f.Close()
	return p
}

func baseDumpCfg() *config.Config {
	return &config.Config{
		DB:          config.DB{Type: config.DBPostgres, Host: "h", User: "u", Password: "p"},
		S3:          config.S3{Bucket: "b", Prefix: "pfx"},
		Backend:     config.BackendS3,
		Periodicity: config.Daily,
	}
}

func TestDumpPipeline_Success(t *testing.T) {
	dir := t.TempDir()
	p := writeGzipFile(t, dir)
	fi, _ := os.Stat(p)

	md := &mockDumper{artifact: &dumper.Artifact{Path: p, Size: fi.Size(), Extension: "sql"}}
	ms := &mockStorage{}
	mn := &mockNotifier{}

	fixedT := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(),
		Dumper: md, Storage: ms, Notifier: mn,
		Clock: clock.Fixed{T: fixedT}, Log: quietLogger(),
	})

	if err := pipe.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if md.called != 1 {
		t.Errorf("dumper called %d times, want 1", md.called)
	}
	if ms.uploadCalls != 1 {
		t.Errorf("upload calls = %d", ms.uploadCalls)
	}
	wantKey := "pfx/daily/2025/03/24/" + filepath.Base(p)
	if ms.uploadedKey != wantKey {
		t.Errorf("uploaded key = %q, want %q", ms.uploadedKey, wantKey)
	}
	if len(mn.events) != 2 {
		t.Fatalf("events = %d, want 2", len(mn.events))
	}
	if mn.events[0].Kind != notify.EventStart {
		t.Errorf("event[0] = %v", mn.events[0].Kind)
	}
	if mn.events[1].Kind != notify.EventSuccess {
		t.Errorf("event[1] = %v", mn.events[1].Kind)
	}
	if mn.events[1].Path == "" || mn.events[1].Size == 0 {
		t.Errorf("success event missing path/size: %+v", mn.events[1])
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("artifact file not removed after pipeline")
	}
}

func TestDumpPipeline_InvalidConfig(t *testing.T) {
	mn := &mockNotifier{}
	pipe := NewDump(DumpDeps{
		Config: &config.Config{},
		Dumper: &mockDumper{}, Storage: &mockStorage{}, Notifier: mn,
		Clock: clock.System{}, Log: quietLogger(),
	})
	err := pipe.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "config") {
		t.Errorf("err = %v", err)
	}
	found := false
	for _, e := range mn.events {
		if e.Kind == notify.EventFailure {
			found = true
		}
	}
	if !found {
		t.Error("expected failure event")
	}
}

func TestDumpPipeline_DumpError(t *testing.T) {
	wantErr := errors.New("dumper exploded")
	md := &mockDumper{err: wantErr}
	mn := &mockNotifier{}
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Storage: &mockStorage{}, Notifier: mn,
		Clock: clock.System{}, Log: quietLogger(),
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v", err)
	}
	found := false
	for _, e := range mn.events {
		if e.Kind == notify.EventFailure && errors.Is(e.Err, err) {
			found = true
		}
	}
	if !found {
		t.Errorf("failure event not received: %+v", mn.events)
	}
}

func TestDumpPipeline_VerifyError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "corrupt.sql.gz")
	_ = os.WriteFile(p, []byte("not gzip"), 0o644)
	md := &mockDumper{artifact: &dumper.Artifact{Path: p, Size: 8, Extension: "sql"}}
	ms := &mockStorage{}
	mn := &mockNotifier{}
	fixedT := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Storage: ms, Notifier: mn,
		Clock: clock.Fixed{T: fixedT}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "exec-vfy", nil },
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, ErrDumpTruncated) {
		t.Errorf("err = %v, want wrap of ErrDumpTruncated", err)
	}
	// Lock must have been released despite verify failure.
	assertLockReleased(t, ms, "pfx/daily/2025/03/24/.lock")
}

// assertLockReleased fails the test if the given lock key wasn't deleted.
func assertLockReleased(t *testing.T, ms *mockStorage, key string) {
	t.Helper()
	for _, d := range ms.deleted {
		if d == key {
			return
		}
	}
	t.Errorf("lock %q not released; deleted=%v", key, ms.deleted)
}

func TestDumpPipeline_UploadError(t *testing.T) {
	dir := t.TempDir()
	p := writeGzipFile(t, dir)
	fi, _ := os.Stat(p)
	md := &mockDumper{artifact: &dumper.Artifact{Path: p, Size: fi.Size(), Extension: "sql"}}
	wantErr := errors.New("upload boom")
	ms := &mockStorage{uploadErr: wantErr}
	mn := &mockNotifier{}
	fixedT := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Storage: ms, Notifier: mn,
		Clock: clock.Fixed{T: fixedT}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "exec-up", nil },
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v", err)
	}
	// Lock must have been released despite upload failure.
	assertLockReleased(t, ms, "pfx/daily/2025/03/24/.lock")
}

func TestDumpPipeline_PanicInDumper_StillReleasesLock(t *testing.T) {
	// Simulate a catastrophic dumper bug via panic. defer lock.Release
	// must still run because Go unwinds the stack through defers on panic.
	md := &panickyDumper{}
	ms := &mockStorage{}
	mn := &mockNotifier{}
	fixedT := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Storage: ms, Notifier: mn,
		Clock: clock.Fixed{T: fixedT}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "exec-panic", nil },
	})

	defer func() {
		// We expect panic to propagate, but the lock must have been released first.
		_ = recover()
		assertLockReleased(t, ms, "pfx/daily/2025/03/24/.lock")
	}()

	_ = pipe.Run(context.Background())
}

// panickyDumper intentionally panics to exercise defer-based lock release.
type panickyDumper struct{}

func (panickyDumper) Dump(_ context.Context) (*dumper.Artifact, error) {
	panic("simulated dumper crash")
}

// mockVerifier is an inline Verifier stub for pipeline tests.
type mockVerifier struct {
	err    error
	called bool
}

func (m *mockVerifier) Verify(_ context.Context, _ string) error {
	m.called = true
	return m.err
}

func TestDumpPipeline_VerifierCalled(t *testing.T) {
	dir := t.TempDir()
	p := writeGzipFile(t, dir)
	fi, _ := os.Stat(p)
	md := &mockDumper{artifact: &dumper.Artifact{Path: p, Size: fi.Size(), Extension: "sql"}}
	mv := &mockVerifier{}
	ms := &mockStorage{}
	mn := &mockNotifier{}
	fixedT := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Verifier: mv, Storage: ms, Notifier: mn,
		Clock: clock.Fixed{T: fixedT}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "exec-vc", nil },
	})
	if err := pipe.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !mv.called {
		t.Error("verifier was not called")
	}
}

func TestDumpPipeline_ContentVerifyFails_AbortsBeforeUpload(t *testing.T) {
	dir := t.TempDir()
	p := writeGzipFile(t, dir)
	fi, _ := os.Stat(p)
	md := &mockDumper{artifact: &dumper.Artifact{Path: p, Size: fi.Size(), Extension: "sql"}}
	wantErr := errors.New("dump truncated")
	mv := &mockVerifier{err: wantErr}
	ms := &mockStorage{}
	mn := &mockNotifier{}
	fixedT := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Verifier: mv, Storage: ms, Notifier: mn,
		Clock: clock.Fixed{T: fixedT}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "exec-cv", nil },
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want content verify error wrapping %v", err, wantErr)
	}
	// Upload must not have been attempted.
	if ms.uploadCalls != 0 {
		t.Errorf("upload called %d times despite verify failure", ms.uploadCalls)
	}
	// Lock must have been released anyway.
	assertLockReleased(t, ms, "pfx/daily/2025/03/24/.lock")
}

func TestDumpPipeline_PreflightUnreachable(t *testing.T) {
	wantErr := errors.New("network partition")
	ms := &mockStorage{listErr: wantErr}
	mn := &mockNotifier{}
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: &mockDumper{}, Storage: ms, Notifier: mn,
		Clock: clock.System{}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "exec-1", nil },
	})
	err := pipe.Run(context.Background())
	if err == nil || !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want unreachable wrapping %v", err, wantErr)
	}
	// No lock should have been written (preflight failed first).
	if len(ms.uploadedBytes) != 0 {
		t.Errorf("unexpected lock writes: %v", ms.uploadedBytes)
	}
}

func TestDumpPipeline_LockAlreadyHeld_Skipped(t *testing.T) {
	dir := t.TempDir()
	p := writeGzipFile(t, dir)
	fi, _ := os.Stat(p)
	md := &mockDumper{artifact: &dumper.Artifact{Path: p, Size: fi.Size(), Extension: "sql"}}
	fixedT := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	cfg := baseDumpCfg()

	// Pre-populate the lock key so Exists returns true.
	wantLock := "pfx/daily/2025/03/24/.lock"
	ms := &mockStorage{existsMap: map[string]bool{wantLock: true}}
	mn := &mockNotifier{}
	pipe := NewDump(DumpDeps{
		Config: cfg, Dumper: md, Storage: ms, Notifier: mn,
		Clock: clock.Fixed{T: fixedT}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "exec-skip", nil },
	})
	// Must exit 0 (nil) — skipped is not a failure.
	if err := pipe.Run(context.Background()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	// Dump must NOT have been called.
	if md.called != 0 {
		t.Errorf("dump was called %d times despite lock", md.called)
	}
	// Notifier must have received EventSkipped.
	foundSkipped := false
	for _, e := range mn.events {
		if e.Kind == notify.EventSkipped {
			foundSkipped = true
			if e.ExecutionID != "exec-skip" {
				t.Errorf("skipped event ExecutionID = %q", e.ExecutionID)
			}
		}
		// Must NOT receive failure.
		if e.Kind == notify.EventFailure {
			t.Errorf("unexpected failure event: %+v", e)
		}
	}
	if !foundSkipped {
		t.Errorf("EventSkipped not received: %+v", mn.events)
	}
}

func TestDumpPipeline_AcquiresAndReleasesLock(t *testing.T) {
	dir := t.TempDir()
	p := writeGzipFile(t, dir)
	fi, _ := os.Stat(p)
	md := &mockDumper{artifact: &dumper.Artifact{Path: p, Size: fi.Size(), Extension: "sql"}}
	fixedT := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	ms := &mockStorage{}
	mn := &mockNotifier{}
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Storage: ms, Notifier: mn,
		Clock: clock.Fixed{T: fixedT}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "exec-acq", nil },
	})
	if err := pipe.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantLock := "pfx/daily/2025/03/24/.lock"
	if _, ok := ms.uploadedBytes[wantLock]; !ok {
		t.Errorf("lock was not written at %s; uploaded=%v", wantLock, ms.uploadedBytes)
	}
	// Lock must have been deleted at the end.
	lockReleased := false
	for _, d := range ms.deleted {
		if d == wantLock {
			lockReleased = true
		}
	}
	if !lockReleased {
		t.Errorf("lock not released; deleted=%v", ms.deleted)
	}
}

func TestDumpPipeline_FailedDump_StillReleasesLock(t *testing.T) {
	wantErr := errors.New("dump boom")
	md := &mockDumper{err: wantErr}
	fixedT := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	ms := &mockStorage{}
	mn := &mockNotifier{}
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Storage: ms, Notifier: mn,
		Clock: clock.Fixed{T: fixedT}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "exec-rel", nil },
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v", err)
	}
	wantLock := "pfx/daily/2025/03/24/.lock"
	lockReleased := false
	for _, d := range ms.deleted {
		if d == wantLock {
			lockReleased = true
		}
	}
	if !lockReleased {
		t.Error("lock not released on failure path")
	}
}
