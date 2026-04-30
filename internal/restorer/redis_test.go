package restorer

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestRedis_Restore_ReturnsSentinel(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBRedis}}
	r := NewRedis(cfg, discardLogger())
	err := r.Restore(context.Background(), "/does/not/exist.rdb.gz")
	if !errors.Is(err, ErrRedisRestoreUnsupported) {
		t.Errorf("err = %v, want ErrRedisRestoreUnsupported", err)
	}
}

func TestRedis_Restore_FromFactory(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBRedis}}
	r, err := New(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Restore(context.Background(), "/any.rdb.gz"); !errors.Is(err, ErrRedisRestoreUnsupported) {
		t.Errorf("err = %v, want ErrRedisRestoreUnsupported", err)
	}
}
