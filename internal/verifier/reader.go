package verifier

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// streamGzipAndTail reads the full compressed file (gzip or zstd, picked by
// extension) and returns up to `tailSize` bytes of the decompressed tail. Any
// decoding error — including truncated trailer / bad CRC — is surfaced, which
// is exactly what we need to catch silently truncated dumps.
//
// The function name is preserved for backwards compat; despite the name it now
// handles `.zst` files too. The dumper picks the codec at write time
// (COMPRESSION_TYPE), and we just mirror that decision on read.
func streamGzipAndTail(gzPath string, tailSize int) ([]byte, error) {
	if tailSize < 1 {
		tailSize = 1
	}
	f, err := os.Open(gzPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", gzPath, err)
	}
	defer f.Close()

	rc, err := openCompressedReader(f, gzPath)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	tail := make([]byte, 0, tailSize)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := rc.Read(buf)
		if n > 0 {
			tail = appendBounded(tail, buf[:n], tailSize)
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return nil, fmt.Errorf("decompress: %w", rerr)
		}
	}
	return tail, nil
}

// openCompressedReader returns an io.ReadCloser that decompresses `r` using
// gzip or zstd depending on the file path suffix. Defaults to gzip when the
// suffix is unknown, mirroring the behavior of the restorer.
func openCompressedReader(r io.Reader, path string) (io.ReadCloser, error) {
	if strings.HasSuffix(path, ".zst") {
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("zstd reader: %w", err)
		}
		return zstdReadCloser{zr}, nil
	}
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	return gr, nil
}

// zstdReadCloser wraps zstd.Decoder so its Close() satisfies io.Closer.
// (zstd.Decoder.Close has no error return.)
type zstdReadCloser struct{ *zstd.Decoder }

func (z zstdReadCloser) Close() error { z.Decoder.Close(); return nil }

// appendBounded appends `chunk` to `tail`, trimming from the front so that
// the result has at most `capacity` bytes.
func appendBounded(tail, chunk []byte, capacity int) []byte {
	combined := append(tail, chunk...)
	if len(combined) > capacity {
		combined = combined[len(combined)-capacity:]
	}
	return combined
}
