package verifier

import (
	"context"
	"strings"
	"testing"
)

func TestRedis_Verify_ValidHeader(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "dump.rdb.gz", "REDIS0011\x00payload bytes\xff")
	if err := NewRedis(discardLogger()).Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestRedis_Verify_BadMagic(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "dump.rdb.gz", "NOPE0011body")
	err := NewRedis(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "redis magic mismatch") {
		t.Errorf("err = %v", err)
	}
}

func TestRedis_Verify_NonDigitVersion(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "dump.rdb.gz", "REDISxxxxbody")
	err := NewRedis(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "ASCII digits") {
		t.Errorf("err = %v", err)
	}
}

func TestRedis_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "dump.rdb.gz", "REDIS0011"+strings.Repeat("x", 128))
	if err := NewRedis(discardLogger()).Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}

func TestRedis_Verify_HeaderTooShort(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "dump.rdb.gz", "RED")
	err := NewRedis(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "read header") {
		t.Errorf("err = %v", err)
	}
}

func TestRedis_RegisteredInFactory(t *testing.T) {
	found := false
	for _, dbt := range Registered() {
		if dbt == "redis" {
			found = true
			break
		}
	}
	if !found {
		t.Error("redis not registered in verifier registry")
	}
}
