// Package dumper defines the Dumper Strategy interface and the produced Artifact.
package dumper

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// Artifact is a compressed dump file on local disk produced by a Dumper.
//
// Checksum is the lower-case hex SHA-256 of the on-disk file and is computed
// by the runner after the dump completes successfully. Storage backends carry
// it through to the uploaded object's metadata so a future Restore can verify
// the artifact end-to-end before applying it.
type Artifact struct {
	Path      string
	Size      int64
	Extension string
	Checksum  string
}

// Verify checks that the compressed stream (gzip or zstd) is readable and not
// empty. The codec is detected from the file extension to keep the call site
// simple — extensions are owned by dumpFilename above.
func (a *Artifact) Verify() error {
	fi, err := os.Stat(a.Path)
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		return fmt.Errorf("dump file is empty: %s", a.Path)
	}
	f, err := os.Open(a.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	var rc io.ReadCloser
	switch {
	case strings.HasSuffix(a.Path, ".zst"):
		zr, err := zstd.NewReader(f)
		if err != nil {
			return fmt.Errorf("corrupt zstd: %w", err)
		}
		rc = zr.IOReadCloser()
	default:
		gr, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("corrupt gzip: %w", err)
		}
		rc = gr
	}
	defer rc.Close()

	buf := make([]byte, 512)
	if _, err := rc.Read(buf); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read compressed stream: %w", err)
	}
	return nil
}

// fileSHA256 computes the lower-case hex SHA-256 of a file's contents.
// Returns "" + nil when the path is empty so callers don't need to special-case.
func fileSHA256(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Cleanup removes the dump file.
func (a *Artifact) Cleanup() error {
	if a.Path == "" {
		return nil
	}
	if err := os.Remove(a.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Dumper is the Strategy interface — one implementation per database engine.
type Dumper interface {
	Dump(ctx context.Context) (*Artifact, error)
}
