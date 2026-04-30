package verifier

import (
	"context"
	"strings"
	"testing"
)

func TestClickhouse_Verify_NonEmpty(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "ch.native.gz", "some Native column bytes")
	if err := NewClickhouse(discardLogger()).Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestClickhouse_Verify_Empty(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "ch.native.gz", "")
	err := NewClickhouse(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("err = %v", err)
	}
}

func TestClickhouse_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "ch.native.gz", "some Native column bytes")
	if err := NewClickhouse(discardLogger()).Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}

func TestClickhouse_RegisteredInFactory(t *testing.T) {
	found := false
	for _, dbt := range Registered() {
		if dbt == "clickhouse" {
			found = true
			break
		}
	}
	if !found {
		t.Error("clickhouse not registered in verifier registry")
	}
}
