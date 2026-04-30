package restorer

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/cloudscript-technology/dumpscript/internal/xerrors"
)

// streamGzipToStdin opens path, decompresses it on the fly (gzip or zstd
// detected by extension), and pipes the plaintext into cmd.Stdin. Used by
// SQL-family restorers.
func streamGzipToStdin(cmd *exec.Cmd, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	dec, decClose, err := decompressReader(f, path)
	if err != nil {
		f.Close() //nolint:errcheck
		return err
	}
	cmd.Stdin = dec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	decErr := decClose()
	fErr := f.Close()
	return xerrors.First(runErr, decErr, fErr)
}

// decompressReader picks gzip or zstd based on the file extension and returns
// a decompressing reader plus a close function (closing the underlying reader
// is the caller's responsibility). Defaults to gzip when the suffix is
// unknown — preserves prior behavior for older artifacts without an explicit
// extension match.
func decompressReader(r io.Reader, path string) (io.Reader, func() error, error) {
	if strings.HasSuffix(path, ".zst") {
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, func() error { return nil }, fmt.Errorf("zstd reader: %w", err)
		}
		return zr, func() error { zr.Close(); return nil }, nil
	}
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, func() error { return nil }, fmt.Errorf("gzip reader: %w", err)
	}
	return gr, gr.Close, nil
}

// streamRawToStdin pipes the raw bytes of gzPath into cmd.Stdin — used by
// mongorestore, which expects the original gzipped archive (--archive --gzip).
func streamRawToStdin(cmd *exec.Cmd, gzPath string) error {
	f, err := os.Open(gzPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", gzPath, err)
	}
	cmd.Stdin = f
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	fErr := f.Close()
	return xerrors.First(runErr, fErr)
}
