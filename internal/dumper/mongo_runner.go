package dumper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cloudscript-technology/dumpscript/internal/xerrors"
)

// runMongoDump pipes mongodump's already-gzipped stdout directly to the file.
// mongodump --archive --gzip writes a compressed archive; we do NOT re-wrap in gzip.Writer.
func runMongoDump(cmd *exec.Cmd, outPath string) (*Artifact, error) {
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("create dump file: %w", err)
	}

	cmd.Stdout = f
	cmd.Stderr = os.Stderr

	runErr := cmd.Run()
	fErr := f.Close()

	if err := xerrors.First(runErr, fErr); err != nil {
		_ = os.Remove(outPath)
		return nil, fmt.Errorf("run mongodump: %w", err)
	}

	fi, err := os.Stat(outPath)
	if err != nil {
		return nil, err
	}
	return &Artifact{Path: outPath, Size: fi.Size(), Extension: "archive"}, nil
}
