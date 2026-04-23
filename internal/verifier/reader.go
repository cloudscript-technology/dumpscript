package verifier

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
)

// streamGzipAndTail reads the full gzip-compressed file and returns up to
// `tailSize` bytes of the decompressed tail. Any gzip decoding error — including
// truncated trailer / bad CRC — is surfaced, which is exactly what we need to
// catch silently truncated dumps.
func streamGzipAndTail(gzPath string, tailSize int) ([]byte, error) {
	if tailSize < 1 {
		tailSize = 1
	}
	f, err := os.Open(gzPath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", gzPath, err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	tail := make([]byte, 0, tailSize)
	buf := make([]byte, 32*1024)
	for {
		n, rerr := gr.Read(buf)
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

// appendBounded appends `chunk` to `tail`, trimming from the front so that
// the result has at most `capacity` bytes.
func appendBounded(tail, chunk []byte, capacity int) []byte {
	combined := append(tail, chunk...)
	if len(combined) > capacity {
		combined = combined[len(combined)-capacity:]
	}
	return combined
}
