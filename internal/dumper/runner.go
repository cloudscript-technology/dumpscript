package dumper

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/xerrors"
)

// dumpFilename returns e.g. ${workDir}/dump_20250324_120000.sql.gz
func dumpFilename(workDir, ext string, now time.Time) string {
	return filepath.Join(workDir, fmt.Sprintf("dump_%s.%s.gz", now.Format("20060102_150405"), ext))
}

// runNativeDump is used by engines that produce their dump in pure Go (e.g.
// HTTP-based sources like Elasticsearch). `write` is called with a writer that
// gzip-compresses into outPath. On any error the partial file is removed.
func runNativeDump(write func(w io.Writer) error, outPath, ext string) (*Artifact, error) {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("create dump file: %w", err)
	}
	gw := gzip.NewWriter(f)

	writeErr := write(gw)
	gzErr := gw.Close()
	fErr := f.Close()

	if err := xerrors.First(writeErr, gzErr, fErr); err != nil {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("run dump: %w", err)
	}
	fi, err := os.Stat(outPath)
	if err != nil {
		return nil, err
	}
	return &Artifact{Path: outPath, Size: fi.Size(), Extension: ext}, nil
}

// runDumpViaTempFile is used by engines whose CLI cannot write to stdout
// (e.g. etcdctl snapshot save). `produce` is given a temp file path; after it
// returns, the temp file is streamed through gzip into outPath.
func runDumpViaTempFile(produce func(tmpPath string) error, outPath, ext string) (*Artifact, error) {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(outPath), "dump_tmp_*."+ext)
	if err != nil {
		return nil, fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	if err := produce(tmpPath); err != nil {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("produce tmp dump: %w", err)
	}

	src, err := os.Open(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("open tmp: %w", err)
	}
	defer src.Close()

	return runNativeDump(func(w io.Writer) error {
		_, err := io.Copy(w, src)
		return err
	}, outPath, ext)
}

// runDumpWithGzip wires cmd.Stdout through gzip.Writer → file, runs the command,
// and returns an Artifact. On any error it removes the partial file.
func runDumpWithGzip(cmd *exec.Cmd, outPath, ext string) (*Artifact, error) {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("create dump file: %w", err)
	}

	gw := gzip.NewWriter(f)
	cmd.Stdout = gw
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	gzErr := gw.Close()
	fErr := f.Close()

	if err := xerrors.First(runErr, gzErr, fErr); err != nil {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("run dump: %w", err)
	}

	fi, err := os.Stat(outPath)
	if err != nil {
		return nil, err
	}
	return &Artifact{Path: outPath, Size: fi.Size(), Extension: ext}, nil
}

