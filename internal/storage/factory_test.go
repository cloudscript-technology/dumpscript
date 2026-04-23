package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestNew_UnknownBackend(t *testing.T) {
	cfg := &config.Config{Backend: config.StorageBackend("quantum")}
	_, err := New(context.Background(), cfg, quietLogger(), Options{})
	if err == nil || !strings.Contains(err.Error(), "no storage registered") {
		t.Fatalf("expected registry lookup error, got %v", err)
	}
}

func TestRegistered_IncludesBothBackends(t *testing.T) {
	reg := Registered()
	has := map[config.StorageBackend]bool{}
	for _, b := range reg {
		has[b] = true
	}
	if !has[config.BackendS3] || !has[config.BackendAzure] {
		t.Errorf("expected s3+azure auto-registered; got %v", reg)
	}
}

func TestNew_AzureSucceeds(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendAzure,
		Azure: config.Azure{
			Account: "acct", Container: "c", Prefix: "p",
			Key: "a2V5", // base64 for "key" — NewSharedKeyCredential validates format, not auth.
		},
	}
	s, err := New(context.Background(), cfg, quietLogger(), Options{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if s == nil {
		t.Fatal("nil storage")
	}
	if s.DisplayPath("dump.sql.gz") == "" {
		t.Error("DisplayPath returned empty")
	}
}
