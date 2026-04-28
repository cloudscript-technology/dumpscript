package dumper

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/xerrors"
)

// dumpFilename returns e.g. ${workDir}/dump_20250324_120000.sql.gz. The base
// suffix is always ".gz" — runners that respect COMPRESSION_TYPE will rewrite
// the suffix to ".zst" when zstd is selected. Engines whose CLI output is
// already a fixed format (mongodump's gzipped archive, etcdctl's binary
// snapshot) keep the .gz suffix regardless.
func dumpFilename(workDir, ext string, now time.Time) string {
	return filepath.Join(workDir, fmt.Sprintf("dump_%s.%s.gz", now.Format("20060102_150405"), ext))
}

// adjustSuffixForCompression swaps the trailing .gz on outPath for the suffix
// matching the active compression codec. Returns the (possibly unchanged) path.
func adjustSuffixForCompression(outPath string, k CompressionKind) string {
	target := compressionSuffix(k)
	if target == ".gz" {
		return outPath
	}
	return outPath[:len(outPath)-len(".gz")] + target
}

// runNativeDump is used by engines that produce their dump in pure Go (e.g.
// HTTP-based sources like Elasticsearch). `write` is called with a writer that
// compresses into outPath using the configured codec (gzip default, zstd when
// COMPRESSION_TYPE=zstd). On any error the partial file is removed.
func runNativeDump(write func(w io.Writer) error, outPath, ext string) (*Artifact, error) {
	codec := resolveCompression()
	outPath = adjustSuffixForCompression(outPath, codec)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("create dump file: %w", err)
	}
	gw, err := newCompressor(f, codec)
	if err != nil {
		f.Close() //nolint:errcheck
		return nil, fmt.Errorf("compressor: %w", err)
	}

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
	sum, err := fileSHA256(outPath)
	if err != nil {
		return nil, fmt.Errorf("checksum: %w", err)
	}
	return &Artifact{Path: outPath, Size: fi.Size(), Extension: ext, Checksum: sum}, nil
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

// runDumpWithGzip wires cmd.Stdout through the configured compressor → file,
// runs the command, and returns an Artifact. The function name is preserved
// for backwards compat; the actual codec depends on COMPRESSION_TYPE.
// On any error it removes the partial file.
func runDumpWithGzip(cmd *exec.Cmd, outPath, ext string) (*Artifact, error) {
	codec := resolveCompression()
	outPath = adjustSuffixForCompression(outPath, codec)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("create dump file: %w", err)
	}

	gw, err := newCompressor(f, codec)
	if err != nil {
		f.Close() //nolint:errcheck
		return nil, fmt.Errorf("compressor: %w", err)
	}
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
	sum, err := fileSHA256(outPath)
	if err != nil {
		return nil, fmt.Errorf("checksum: %w", err)
	}
	return &Artifact{Path: outPath, Size: fi.Size(), Extension: ext, Checksum: sum}, nil
}

