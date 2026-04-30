package verifier

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// redisMagic is the 5-byte ASCII header every RDB file starts with.
var redisMagic = []byte("REDIS")

func init() {
	Register(config.DBRedis, func(log *slog.Logger) Verifier { return NewRedis(log) })
}

// Redis verifies an RDB snapshot produced by `redis-cli --rdb -`.
// Checks:
//   - gzip decodes end-to-end (catches truncation via CRC/ISIZE trailer)
//   - first 5 bytes of the decoded stream are "REDIS"
//   - following 4 bytes (version) are ASCII digits
type Redis struct {
	log *slog.Logger
}

func NewRedis(log *slog.Logger) *Redis { return &Redis{log: log} }

func (r *Redis) Verify(_ context.Context, gzPath string) error {
	f, err := os.Open(gzPath)
	if err != nil {
		return fmt.Errorf("redis verify open: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("redis verify gzip: %w", err)
	}
	defer gr.Close()

	var header [9]byte
	if _, err := io.ReadFull(gr, header[:]); err != nil {
		return fmt.Errorf("redis verify read header: %w", err)
	}
	if !bytes.Equal(header[:5], redisMagic) {
		return fmt.Errorf("redis magic mismatch: got %q, want %q", header[:5], redisMagic)
	}
	for _, b := range header[5:] {
		if b < '0' || b > '9' {
			return fmt.Errorf("redis version bytes are not ASCII digits: %q", header[5:])
		}
	}

	if _, err := io.Copy(io.Discard, gr); err != nil {
		return fmt.Errorf("redis verify stream: %w", err)
	}
	r.log.Debug("redis content verified", "path", gzPath, "version", string(header[5:]))
	return nil
}
