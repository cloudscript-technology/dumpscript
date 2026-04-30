package verifier

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMongoArchive writes a gzipped mongodump archive (magic + body).
// Pass writeMagic=false to produce an invalid-magic file.
func writeMongoArchive(t *testing.T, dir, name string, writeMagic bool, body string) string {
	t.Helper()
	var plain bytes.Buffer
	if writeMagic {
		_ = binary.Write(&plain, binary.LittleEndian, mongoArchiveMagic)
	} else {
		plain.Write([]byte{0, 0, 0, 0})
	}
	plain.WriteString(body)

	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(plain.Bytes())
	_ = gw.Close()

	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, gz.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMongo_Verify_ValidArchive(t *testing.T) {
	dir := t.TempDir()
	p := writeMongoArchive(t, dir, "m.archive.gz", true, "archive body bytes")
	v := NewMongo(discardLogger())
	if err := v.Verify(context.Background(), p); err != nil {
		t.Errorf("unexpected: %v", err)
	}
}

func TestMongo_Verify_InvalidMagic(t *testing.T) {
	dir := t.TempDir()
	p := writeMongoArchive(t, dir, "m.archive.gz", false, "body")
	v := NewMongo(discardLogger())
	err := v.Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "magic mismatch") {
		t.Errorf("err = %v", err)
	}
}

func TestMongo_Verify_NotAGzip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "plain.gz")
	_ = os.WriteFile(p, []byte("not gzip at all"), 0o644)
	v := NewMongo(discardLogger())
	if err := v.Verify(context.Background(), p); err == nil {
		t.Error("expected gzip error")
	}
}

func TestMongo_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	var plain bytes.Buffer
	_ = binary.Write(&plain, binary.LittleEndian, mongoArchiveMagic)
	plain.WriteString("important archive body")
	var gz bytes.Buffer
	gw := gzip.NewWriter(&gz)
	_, _ = gw.Write(plain.Bytes())
	_ = gw.Close()
	p := filepath.Join(dir, "m.archive.gz")
	_ = os.WriteFile(p, gz.Bytes()[:len(gz.Bytes())-12], 0o644)

	v := NewMongo(discardLogger())
	if err := v.Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated archive")
	}
}

func TestMongo_Verify_MissingFile(t *testing.T) {
	v := NewMongo(discardLogger())
	if err := v.Verify(context.Background(), "/nonexistent-mongo-test-path.gz"); err == nil {
		t.Error("expected open error")
	}
}

func TestMongoArchiveMagic_Value(t *testing.T) {
	if mongoArchiveMagic != 0x8199e26d {
		t.Errorf("mongoArchiveMagic = %#x", mongoArchiveMagic)
	}
}
