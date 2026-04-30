package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

type mockRestorer struct {
	err        error
	calledWith string
}

func (m *mockRestorer) Restore(_ context.Context, gzPath string) error {
	m.calledWith = gzPath
	return m.err
}

func baseRestoreCfg(t *testing.T) *config.Config {
	return &config.Config{
		DB:      config.DB{Type: config.DBPostgres, Host: "h", User: "u", Password: "p"},
		S3:      config.S3{Bucket: "b", Prefix: "pfx", Key: "pfx/daily/2025/03/24/dump.sql.gz"},
		Backend: config.BackendS3,
		WorkDir: t.TempDir(),
	}
}

func TestRestorePipeline_Success(t *testing.T) {
	cfg := baseRestoreCfg(t)
	ms := &mockStorage{}
	mr := &mockRestorer{}
	pipe := NewRestore(RestoreDeps{
		Config: cfg, Restorer: mr, Storage: ms, Log: quietLogger(),
	})
	if err := pipe.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if ms.downloadDst == "" {
		t.Errorf("download dst not set")
	}
	if mr.calledWith == "" {
		t.Errorf("restorer not called")
	}
	if !strings.HasSuffix(mr.calledWith, ".sql.gz") {
		t.Errorf("restorer called with: %q", mr.calledWith)
	}
}

func TestRestorePipeline_MongoExtension(t *testing.T) {
	cfg := baseRestoreCfg(t)
	cfg.DB.Type = config.DBMongo
	ms := &mockStorage{}
	mr := &mockRestorer{}
	pipe := NewRestore(RestoreDeps{
		Config: cfg, Restorer: mr, Storage: ms, Log: quietLogger(),
	})
	if err := pipe.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.HasSuffix(mr.calledWith, ".archive.gz") {
		t.Errorf("expected .archive.gz suffix, got %q", mr.calledWith)
	}
}

func TestRestorePipeline_InvalidConfig(t *testing.T) {
	cfg := baseRestoreCfg(t)
	cfg.S3.Key = ""
	pipe := NewRestore(RestoreDeps{
		Config: cfg, Restorer: &mockRestorer{}, Storage: &mockStorage{}, Log: quietLogger(),
	})
	err := pipe.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "config") {
		t.Errorf("err = %v", err)
	}
}

func TestRestorePipeline_DownloadError(t *testing.T) {
	cfg := baseRestoreCfg(t)
	wantErr := errors.New("download boom")
	ms := &mockStorage{downloadErr: wantErr}
	pipe := NewRestore(RestoreDeps{
		Config: cfg, Restorer: &mockRestorer{}, Storage: ms, Log: quietLogger(),
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v", err)
	}
}

func TestRestorePipeline_RestoreError(t *testing.T) {
	cfg := baseRestoreCfg(t)
	wantErr := errors.New("psql failed")
	pipe := NewRestore(RestoreDeps{
		Config: cfg, Restorer: &mockRestorer{err: wantErr}, Storage: &mockStorage{}, Log: quietLogger(),
	})
	err := pipe.Run(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v", err)
	}
}
