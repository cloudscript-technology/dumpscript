package verifier

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// oracleMagic is the ASCII marker Oracle `exp` writes near the start of every
// DMP file. A small amount of binary noise may precede it, so we scan the
// first 512 bytes rather than requiring an exact offset.
var oracleMagic = []byte("EXPORT:V")

func init() {
	Register(config.DBOracle, func(log *slog.Logger) Verifier { return NewOracle(log) })
}

type Oracle struct {
	log *slog.Logger
}

func NewOracle(log *slog.Logger) *Oracle { return &Oracle{log: log} }

func (o *Oracle) Verify(_ context.Context, gzPath string) error {
	f, err := os.Open(gzPath)
	if err != nil {
		return fmt.Errorf("oracle verify open: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("oracle verify gzip: %w", err)
	}
	defer gr.Close()

	var head [512]byte
	n, err := io.ReadFull(gr, head[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return fmt.Errorf("oracle verify read head: %w", err)
	}
	if !bytes.Contains(head[:n], oracleMagic) {
		return fmt.Errorf("oracle DMP magic %q not found in first %d bytes; not an exp dump or corrupted", oracleMagic, n)
	}

	// Drain the remainder so the gzip CRC/ISIZE trailer is validated —
	// catches SIGKILL-truncated streams that still wrote a valid header.
	if _, err := io.Copy(io.Discard, gr); err != nil {
		return fmt.Errorf("oracle verify stream: %w", err)
	}
	o.log.Debug("oracle content verified", "path", gzPath)
	return nil
}
