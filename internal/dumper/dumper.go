// Package dumper defines the Dumper Strategy interface and the produced Artifact.
package dumper

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// Artifact is a compressed dump file on local disk produced by a Dumper.
type Artifact struct {
	Path      string
	Size      int64
	Extension string
}

// Verify checks that the gzip stream is readable and not empty.
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
	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("corrupt gzip: %w", err)
	}
	defer gr.Close()
	buf := make([]byte, 512)
	if _, err := gr.Read(buf); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read gzip: %w", err)
	}
	return nil
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
