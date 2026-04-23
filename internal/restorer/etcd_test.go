package restorer

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestEtcd_Restore_ReturnsSentinel(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBEtcd}}
	r := NewEtcd(cfg, discardLogger())
	err := r.Restore(context.Background(), "/does/not/exist.db.gz")
	if !errors.Is(err, ErrEtcdRestoreUnsupported) {
		t.Errorf("err = %v, want ErrEtcdRestoreUnsupported", err)
	}
}

func TestEtcd_Restore_FromFactory(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBEtcd}}
	r, err := New(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Restore(context.Background(), "/any.db.gz"); !errors.Is(err, ErrEtcdRestoreUnsupported) {
		t.Errorf("err = %v, want ErrEtcdRestoreUnsupported", err)
	}
}
