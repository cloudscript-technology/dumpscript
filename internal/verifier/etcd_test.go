package verifier

import (
	"context"
	"strings"
	"testing"
)

func etcdPayload(magic []byte) string {
	body := make([]byte, 0, 16+4+4096)
	body = append(body, make([]byte, 16)...)
	body = append(body, magic...)
	body = append(body, make([]byte, 4096)...)
	return string(body)
}

func TestEtcd_Verify_ValidLEMagic(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "etcd.db.gz", etcdPayload(boltMagicLE))
	if err := NewEtcd(discardLogger()).Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestEtcd_Verify_ValidBEMagic(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "etcd.db.gz", etcdPayload(boltMagicBE))
	if err := NewEtcd(discardLogger()).Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestEtcd_Verify_MissingMagic(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "etcd.db.gz", strings.Repeat("\x00", 5000))
	err := NewEtcd(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestEtcd_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "etcd.db.gz", etcdPayload(boltMagicLE))
	if err := NewEtcd(discardLogger()).Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}

func TestEtcd_RegisteredInFactory(t *testing.T) {
	found := false
	for _, dbt := range Registered() {
		if dbt == "etcd" {
			found = true
			break
		}
	}
	if !found {
		t.Error("etcd not registered in verifier registry")
	}
}
