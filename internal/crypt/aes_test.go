package crypt

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func newKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "plain.bin")
	enc := filepath.Join(dir, "cipher.bin")
	dec := filepath.Join(dir, "round.bin")

	original := bytes.Repeat([]byte("dumpscript "), 1024) // ~11KB
	if err := os.WriteFile(src, original, 0o600); err != nil {
		t.Fatal(err)
	}
	key := newKey(t)

	if err := EncryptFile(src, enc, key); err != nil {
		t.Fatalf("EncryptFile: %v", err)
	}
	if err := DecryptFile(enc, dec, key); err != nil {
		t.Fatalf("DecryptFile: %v", err)
	}
	got, err := os.ReadFile(dec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("round-trip mismatch: got %d bytes, want %d", len(got), len(original))
	}
}

func TestDecryptFile_WrongKey(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p.bin")
	enc := filepath.Join(dir, "c.bin")
	dec := filepath.Join(dir, "d.bin")
	if err := os.WriteFile(src, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EncryptFile(src, enc, newKey(t)); err != nil {
		t.Fatal(err)
	}
	wrong := newKey(t)
	if err := DecryptFile(enc, dec, wrong); err == nil {
		t.Fatal("expected decrypt with wrong key to fail")
	}
	if _, err := os.Stat(dec); !os.IsNotExist(err) {
		t.Errorf("dst should not exist after failed decrypt, got err=%v", err)
	}
}

func TestDecryptFile_Tampered(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p.bin")
	enc := filepath.Join(dir, "c.bin")
	dec := filepath.Join(dir, "d.bin")
	key := newKey(t)
	if err := os.WriteFile(src, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EncryptFile(src, enc, key); err != nil {
		t.Fatal(err)
	}
	// Flip one byte after the nonce — should fail GCM auth.
	raw, _ := os.ReadFile(enc)
	raw[len(raw)-1] ^= 0xFF
	_ = os.WriteFile(enc, raw, 0o600)

	if err := DecryptFile(enc, dec, key); err == nil {
		t.Fatal("expected decrypt of tampered ciphertext to fail")
	}
}

func TestLoadKey_Hex(t *testing.T) {
	dir := t.TempDir()
	raw := newKey(t)
	hexFile := filepath.Join(dir, "k.hex")
	if err := os.WriteFile(hexFile, []byte(hex.EncodeToString(raw)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadKey(hexFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("hex round-trip mismatch")
	}
}

func TestLoadKey_RawBytes(t *testing.T) {
	dir := t.TempDir()
	raw := newKey(t)
	binFile := filepath.Join(dir, "k.bin")
	if err := os.WriteFile(binFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadKey(binFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("raw round-trip mismatch")
	}
}

func TestLoadKey_BadLength(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte("too short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey(bad); err == nil {
		t.Fatal("expected error for invalid key length")
	}
}
