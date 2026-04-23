package restorer

import (
	"compress/gzip"
	"fmt"
	"os"
	"os/exec"

	"github.com/cloudscript-technology/dumpscript/internal/xerrors"
)

// streamGzipToStdin opens gzPath, decompresses it on the fly, and pipes the
// plaintext into cmd.Stdin. Used by SQL-family restorers.
func streamGzipToStdin(cmd *exec.Cmd, gzPath string) error {
	f, err := os.Open(gzPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", gzPath, err)
	}
	gr, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return fmt.Errorf("gzip reader: %w", err)
	}
	cmd.Stdin = gr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	grErr := gr.Close()
	fErr := f.Close()
	return xerrors.First(runErr, grErr, fErr)
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
