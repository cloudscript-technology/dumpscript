package verifier

import (
	"context"
	"strings"
	"testing"
)

func oracleValidPayload() string {
	return "\x03\x00EXPORT:V19.00.00.00.00\x00" + strings.Repeat("\x00", 600)
}

func TestOracle_Verify_ValidHeader(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "d.dmp.gz", oracleValidPayload())
	if err := NewOracle(discardLogger()).Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestOracle_Verify_MissingMagic(t *testing.T) {
	dir := t.TempDir()
	body := "\x00\x00NOT-AN-ORACLE-DUMP\x00" + strings.Repeat("x", 600)
	p := writeGzip(t, dir, "d.dmp.gz", body)
	err := NewOracle(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestOracle_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "d.dmp.gz", oracleValidPayload())
	if err := NewOracle(discardLogger()).Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}

func TestOracle_Verify_ShortHeaderMissingMagic(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "d.dmp.gz", "\x01\x02\x03")
	err := NewOracle(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestOracle_RegisteredInFactory(t *testing.T) {
	found := false
	for _, dbt := range Registered() {
		if dbt == "oracle" {
			found = true
			break
		}
	}
	if !found {
		t.Error("oracle not registered in verifier registry")
	}
}
