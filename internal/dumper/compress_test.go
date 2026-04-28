package dumper

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestResolveCompression_DefaultsToGzip(t *testing.T) {
	t.Setenv("COMPRESSION_TYPE", "")
	if got := resolveCompression(); got != CompressionGzip {
		t.Fatalf("default = %q, want gzip", got)
	}
}

func TestResolveCompression_Zstd(t *testing.T) {
	t.Setenv("COMPRESSION_TYPE", "zstd")
	if got := resolveCompression(); got != CompressionZstd {
		t.Fatalf("zstd = %q, want zstd", got)
	}
}

func TestResolveCompression_UnknownFallsBackToGzip(t *testing.T) {
	t.Setenv("COMPRESSION_TYPE", "lz4")
	if got := resolveCompression(); got != CompressionGzip {
		t.Fatalf("unknown = %q, want gzip fallback", got)
	}
}

func TestCompressionSuffix(t *testing.T) {
	if got := compressionSuffix(CompressionGzip); got != ".gz" {
		t.Fatalf("gzip suffix = %q, want .gz", got)
	}
	if got := compressionSuffix(CompressionZstd); got != ".zst" {
		t.Fatalf("zstd suffix = %q, want .zst", got)
	}
}

func TestNewCompressor_Gzip_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w, err := newCompressor(&buf, CompressionGzip)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello world\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	gr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("output is not valid gzip: %v", err)
	}
	defer gr.Close()
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world\n" {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

func TestNewCompressor_Zstd_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	w, err := newCompressor(&buf, CompressionZstd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("ola mundo\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	zr, err := zstd.NewReader(&buf)
	if err != nil {
		t.Fatalf("output is not valid zstd: %v", err)
	}
	defer zr.Close()
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ola mundo\n" {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

func TestAdjustSuffixForCompression(t *testing.T) {
	cases := []struct {
		in   string
		k    CompressionKind
		want string
	}{
		{"/work/dump_TS.sql.gz", CompressionGzip, "/work/dump_TS.sql.gz"},
		{"/work/dump_TS.sql.gz", CompressionZstd, "/work/dump_TS.sql.zst"},
		{"/work/dump_TS.archive.gz", CompressionZstd, "/work/dump_TS.archive.zst"},
	}
	for _, tc := range cases {
		got := adjustSuffixForCompression(tc.in, tc.k)
		if got != tc.want {
			t.Errorf("adjustSuffixForCompression(%q, %q) = %q, want %q",
				tc.in, tc.k, got, tc.want)
		}
	}
}

// TestZstdSuffix_UsedByRunner sanity-checks the integration: when
// COMPRESSION_TYPE=zstd the path produced by adjustSuffixForCompression ends
// in .zst even if the input said .gz.
func TestZstdSuffix_UsedByRunner(t *testing.T) {
	t.Setenv("COMPRESSION_TYPE", "zstd")
	in := "/tmp/dump_X.sql.gz"
	got := adjustSuffixForCompression(in, resolveCompression())
	if !strings.HasSuffix(got, ".zst") {
		t.Fatalf("expected .zst suffix, got %q", got)
	}
}
