package verifier

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func writeGzip(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	if _, err := gw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeTruncatedGzip writes a gzip file whose last 12 bytes (CRC32+ISIZE
// trailer) are missing — simulating SIGKILL mid-write.
func writeTruncatedGzip(t *testing.T, dir, name, content string) string {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	_, _ = gw.Write([]byte(content))
	_ = gw.Close()
	full := buf.Bytes()
	truncated := full[:len(full)-12]
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, truncated, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStreamGzipAndTail_FullContent(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "x.gz", "hello world")
	tail, err := streamGzipAndTail(p, 4096)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if string(tail) != "hello world" {
		t.Errorf("tail = %q", tail)
	}
}

func TestStreamGzipAndTail_SmallTailKeepsLastN(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "x.gz", "abcdefghijklmnopqrstuvwxyz")
	tail, err := streamGzipAndTail(p, 5)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if string(tail) != "vwxyz" {
		t.Errorf("tail = %q, want vwxyz", tail)
	}
}

func TestStreamGzipAndTail_TruncatedGzipFails(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "bad.gz", "important content here")
	_, err := streamGzipAndTail(p, 4096)
	if err == nil {
		t.Fatal("expected error for truncated gzip")
	}
}

func TestStreamGzipAndTail_NotAGzip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "not-gzip.gz")
	_ = os.WriteFile(p, []byte("plain text not gzip"), 0o644)
	_, err := streamGzipAndTail(p, 4096)
	if err == nil {
		t.Fatal("expected error for non-gzip")
	}
}

func TestStreamGzipAndTail_MissingFile(t *testing.T) {
	_, err := streamGzipAndTail("/nonexistent/verifier/test/path", 4096)
	if err == nil {
		t.Fatal("expected open error")
	}
}

func TestAppendBounded(t *testing.T) {
	tests := []struct {
		name        string
		tail, chunk []byte
		capacity    int
		want        string
	}{
		{"empty tail", []byte{}, []byte("abc"), 10, "abc"},
		{"under capacity", []byte("abc"), []byte("def"), 10, "abcdef"},
		{"at capacity", []byte("abc"), []byte("def"), 6, "abcdef"},
		{"overflow trims front", []byte("abc"), []byte("defgh"), 5, "defgh"},
		{"larger overflow", []byte("abcde"), []byte("fgh"), 5, "defgh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := appendBounded(tc.tail, tc.chunk, tc.capacity)
			if string(got) != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
