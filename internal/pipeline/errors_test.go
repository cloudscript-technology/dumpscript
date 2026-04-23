package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/clock"
	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/dumper"
	"github.com/cloudscript-technology/dumpscript/internal/lock"
)

func TestSentinel_ErrLockHeldIsLockErrLocked(t *testing.T) {
	if !errors.Is(ErrLockHeld, lock.ErrLocked) {
		t.Error("ErrLockHeld does not match lock.ErrLocked")
	}
}

func TestSentinel_ConfigInvalid(t *testing.T) {
	pipe := NewDump(DumpDeps{
		Config: &config.Config{}, // missing everything
		Dumper: &mockDumper{}, Storage: &mockStorage{}, Notifier: &mockNotifier{},
		Clock: clock.System{}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "e1", nil },
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, ErrConfigInvalid) {
		t.Errorf("err = %v, want ErrConfigInvalid", err)
	}
}

func TestSentinel_DestinationUnreachable(t *testing.T) {
	wantCause := errors.New("network partition")
	ms := &mockStorage{listErr: wantCause}
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: &mockDumper{}, Storage: ms, Notifier: &mockNotifier{},
		Clock: clock.System{}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "e1", nil },
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, ErrDestinationUnreachable) {
		t.Errorf("expected ErrDestinationUnreachable, got %v", err)
	}
	if !errors.Is(err, wantCause) {
		t.Errorf("underlying cause lost: %v", err)
	}
}

func TestSentinel_DumpFailed(t *testing.T) {
	wantCause := errors.New("pg_dump crashed")
	md := &mockDumper{err: wantCause}
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Storage: &mockStorage{}, Notifier: &mockNotifier{},
		Clock: clock.Fixed{T: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "e1", nil },
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, ErrDumpFailed) {
		t.Errorf("expected ErrDumpFailed, got %v", err)
	}
	if !errors.Is(err, wantCause) {
		t.Errorf("cause lost: %v", err)
	}
}

func TestSentinel_DumpTruncated_ViaCorruptGzip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "corrupt.sql.gz")
	_ = os.WriteFile(p, []byte("not gzip"), 0o644)
	md := &mockDumper{artifact: &dumper.Artifact{Path: p, Size: 8, Extension: "sql"}}
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Storage: &mockStorage{}, Notifier: &mockNotifier{},
		Clock: clock.Fixed{T: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "e1", nil },
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, ErrDumpTruncated) {
		t.Errorf("expected ErrDumpTruncated, got %v", err)
	}
}

func TestSentinel_UploadFailed(t *testing.T) {
	dir := t.TempDir()
	p := writeGzipFile(t, dir)
	fi, _ := os.Stat(p)
	wantCause := errors.New("s3 500")
	md := &mockDumper{artifact: &dumper.Artifact{Path: p, Size: fi.Size(), Extension: "sql"}}
	ms := &mockStorage{uploadErr: wantCause}
	pipe := NewDump(DumpDeps{
		Config: baseDumpCfg(), Dumper: md, Storage: ms, Notifier: &mockNotifier{},
		Clock: clock.Fixed{T: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}, Log: quietLogger(),
		NewExecutionID: func() (string, error) { return "e1", nil },
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, ErrUploadFailed) {
		t.Errorf("expected ErrUploadFailed, got %v", err)
	}
	if !errors.Is(err, wantCause) {
		t.Errorf("cause lost: %v", err)
	}
}
