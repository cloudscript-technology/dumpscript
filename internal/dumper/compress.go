package dumper

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// CompressionKind enumerates the supported on-disk compression formats.
// gzip is the default for backwards compatibility; zstd produces ~30% smaller
// dumps at ~2x the throughput on modern CPUs.
type CompressionKind string

const (
	CompressionGzip CompressionKind = "gzip"
	CompressionZstd CompressionKind = "zstd"
)

// resolveCompression returns the compression kind selected by COMPRESSION_TYPE.
// Defaults to gzip. Unknown values fall back to gzip with a logged warning at
// the call site (we deliberately don't import slog here to keep this trivial).
func resolveCompression() CompressionKind {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("COMPRESSION_TYPE")))
	switch v {
	case "", string(CompressionGzip):
		return CompressionGzip
	case string(CompressionZstd):
		return CompressionZstd
	default:
		// Unknown value — fall back to gzip rather than fail. Engines log
		// the choice at the runner level.
		return CompressionGzip
	}
}

// compressionSuffix returns the file extension (with leading dot) appended to
// dump filenames so consumers can recognize the encoding without sniffing
// magic bytes (.gz, .zst).
func compressionSuffix(k CompressionKind) string {
	switch k {
	case CompressionZstd:
		return ".zst"
	default:
		return ".gz"
	}
}

// newCompressor returns an io.WriteCloser that compresses input using the
// selected algorithm and writes the compressed stream to w. Closing the
// returned writer flushes the compressor; closing the *underlying* w is the
// caller's responsibility.
func newCompressor(w io.Writer, k CompressionKind) (io.WriteCloser, error) {
	switch k {
	case CompressionZstd:
		// Level 3 is zstd's "default": good ratio, ~500MB/s on modern CPUs.
		// Higher levels add ratio but cost more CPU; not worth it for backup
		// workloads where the upload is usually the bottleneck.
		enc, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault))
		if err != nil {
			return nil, fmt.Errorf("zstd writer: %w", err)
		}
		return enc, nil
	default:
		return gzip.NewWriter(w), nil
	}
}
