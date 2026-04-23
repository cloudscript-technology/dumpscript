package storage

import (
	"context"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestAzure_DisplayPath(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendAzure,
		Azure:   config.Azure{Account: "acct", Container: "c", Key: "a2V5"},
	}
	a, err := NewAzure(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}
	got := a.DisplayPath("dumps/daily/2025/03/24/dump.sql.gz")
	want := "azure://c/dumps/daily/2025/03/24/dump.sql.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAzure_New_SharedKey(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendAzure,
		Azure:   config.Azure{Account: "acct", Container: "c", Key: "a2V5"},
	}
	a, err := NewAzure(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}
	if a == nil {
		t.Fatal("nil client")
	}
}

func TestAzure_New_SAS(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendAzure,
		Azure:   config.Azure{Account: "acct", Container: "c", SASToken: "sv=2020&sig=X"},
	}
	a, err := NewAzure(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewAzure (SAS): %v", err)
	}
	if a == nil {
		t.Fatal("nil client")
	}
}

func TestAzure_New_InvalidSharedKey(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendAzure,
		Azure:   config.Azure{Account: "acct", Container: "c", Key: "not-valid-base64!!!"},
	}
	_, err := NewAzure(context.Background(), cfg, quietLogger())
	if err == nil {
		t.Fatal("expected error for invalid shared key")
	}
}
