package dumper

import (
	"compress/gzip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDumpFilename(t *testing.T) {
	ts := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	got := dumpFilename("/work", "sql", ts)
	want := "/work/dump_20250324_120000.sql.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	got2 := dumpFilename("/w", "archive", ts)
	if !strings.HasSuffix(got2, ".archive.gz") {
		t.Errorf("bad ext: %s", got2)
	}
}

func TestRunDumpWithGzip_Success(t *testing.T) {
	if _, err := exec.LookPath("echo"); err != nil {
		t.Skip("echo not available")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "dump.sql.gz")

	cmd := exec.Command("echo", "-n", "hello dumpscript")
	art, err := runDumpWithGzip(cmd, out, "sql")
	if err != nil {
		t.Fatalf("runDumpWithGzip: %v", err)
	}
	if art.Path != out {
		t.Errorf("path = %s, want %s", art.Path, out)
	}
	if art.Extension != "sql" {
		t.Errorf("extension = %s", art.Extension)
	}
	if art.Size <= 0 {
		t.Errorf("size = %d", art.Size)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	body, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello dumpscript" {
		t.Errorf("content = %q", body)
	}
}

func TestRunDumpWithGzip_CommandFails(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "dump.sql.gz")

	cmd := exec.Command("/nonexistent-binary-dumpscript-test")
	_, err := runDumpWithGzip(cmd, out, "sql")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("expected partial file removed, stat err: %v", statErr)
	}
}

func TestArtifact_Verify(t *testing.T) {
	dir := t.TempDir()

	t.Run("valid gzip", func(t *testing.T) {
		p := filepath.Join(dir, "ok.sql.gz")
		f, _ := os.Create(p)
		gw := gzip.NewWriter(f)
		_, _ = gw.Write([]byte("SELECT 1;"))
		_ = gw.Close()
		_ = f.Close()
		art := &Artifact{Path: p}
		if err := art.Verify(); err != nil {
			t.Errorf("unexpected: %v", err)
		}
	})

	t.Run("empty file", func(t *testing.T) {
		p := filepath.Join(dir, "empty.sql.gz")
		_ = os.WriteFile(p, []byte{}, 0o644)
		art := &Artifact{Path: p}
		if err := art.Verify(); err == nil || !strings.Contains(err.Error(), "empty") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("corrupt gzip", func(t *testing.T) {
		p := filepath.Join(dir, "bad.sql.gz")
		_ = os.WriteFile(p, []byte("not a gzip"), 0o644)
		art := &Artifact{Path: p}
		if err := art.Verify(); err == nil || !strings.Contains(err.Error(), "corrupt") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		art := &Artifact{Path: filepath.Join(dir, "does-not-exist")}
		if err := art.Verify(); err == nil {
			t.Error("expected error for missing file")
		}
	})
}

func TestArtifact_Cleanup(t *testing.T) {
	dir := t.TempDir()

	t.Run("removes existing", func(t *testing.T) {
		p := filepath.Join(dir, "x.gz")
		_ = os.WriteFile(p, []byte("data"), 0o644)
		art := &Artifact{Path: p}
		if err := art.Cleanup(); err != nil {
			t.Errorf("unexpected: %v", err)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Error("file not removed")
		}
	})

	t.Run("missing file is not an error", func(t *testing.T) {
		art := &Artifact{Path: filepath.Join(dir, "missing")}
		if err := art.Cleanup(); err != nil {
			t.Errorf("unexpected: %v", err)
		}
	})

	t.Run("empty path is noop", func(t *testing.T) {
		art := &Artifact{}
		if err := art.Cleanup(); err != nil {
			t.Errorf("unexpected: %v", err)
		}
	})
}
