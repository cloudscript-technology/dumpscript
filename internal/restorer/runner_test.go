package restorer

import (
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func makeGzip(t *testing.T, dir, name, content string) string {
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

func TestStreamGzipToStdin_Success(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}
	dir := t.TempDir()
	src := makeGzip(t, dir, "input.sql.gz", "SELECT 1;")

	cmd := exec.Command("cat")
	if err := streamGzipToStdin(cmd, src); err != nil {
		t.Fatalf("streamGzipToStdin: %v", err)
	}
}

func TestStreamGzipToStdin_OpenError(t *testing.T) {
	cmd := exec.Command("cat")
	if err := streamGzipToStdin(cmd, "/nonexistent-path-dumpscript-test"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestStreamGzipToStdin_BadGzip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.gz")
	_ = os.WriteFile(p, []byte("not gzip"), 0o644)
	cmd := exec.Command("cat")
	if err := streamGzipToStdin(cmd, p); err == nil {
		t.Error("expected gzip decode error")
	}
}

func TestStreamRawToStdin_Success(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "raw.bin")
	_ = os.WriteFile(p, []byte("binary content"), 0o644)
	cmd := exec.Command("cat")
	if err := streamRawToStdin(cmd, p); err != nil {
		t.Fatalf("streamRawToStdin: %v", err)
	}
}

func TestStreamRawToStdin_OpenError(t *testing.T) {
	cmd := exec.Command("cat")
	if err := streamRawToStdin(cmd, "/nonexistent-dumpscript-restore"); err == nil {
		t.Error("expected error")
	}
}
