package verifier

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBClickhouse, func(log *slog.Logger) Verifier { return NewClickhouse(log) })
}

// Clickhouse Native binary has no stable magic bytes (it is a serialized
// column stream). The best envelope-level check is to drain the full gzip
// stream — a SIGKILL mid-write leaves a truncated gzip trailer — and assert
// the decompressed output is non-empty.
type Clickhouse struct {
	log *slog.Logger
}

func NewClickhouse(log *slog.Logger) *Clickhouse { return &Clickhouse{log: log} }

func (c *Clickhouse) Verify(_ context.Context, gzPath string) error {
	tail, err := streamGzipAndTail(gzPath, 4096)
	if err != nil {
		return fmt.Errorf("clickhouse verify: %w", err)
	}
	if len(tail) == 0 {
		return fmt.Errorf("clickhouse dump is empty")
	}
	c.log.Debug("clickhouse content verified", "path", gzPath, "tail_bytes", len(tail))
	return nil
}
