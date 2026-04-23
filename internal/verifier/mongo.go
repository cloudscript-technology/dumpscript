package verifier

import (
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBMongo, func(log *slog.Logger) Verifier { return NewMongo(log) })
}

// mongoArchiveMagic is the 4-byte little-endian header mongodump writes at the
// start of an --archive stream. If it's missing/wrong, the file is not a valid
// mongodump archive.
const mongoArchiveMagic uint32 = 0x8199e26d

// Mongo verifies a mongodump --archive --gzip artifact. It ensures:
//   - the gzip stream decodes cleanly end-to-end (truncation fails here)
//   - the decoded archive starts with the mongodump magic number
//
// We intentionally avoid invoking `mongorestore --dryRun` because that tool
// requires a reachable MongoDB server; a pure-file check keeps dumpscript
// self-contained and fast.
type Mongo struct {
	log *slog.Logger
}

func NewMongo(log *slog.Logger) *Mongo { return &Mongo{log: log} }

func (m *Mongo) Verify(_ context.Context, gzPath string) error {
	f, err := os.Open(gzPath)
	if err != nil {
		return fmt.Errorf("mongo verify open: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("mongo verify gzip: %w", err)
	}
	defer gr.Close()

	var magicBuf [4]byte
	if _, err := io.ReadFull(gr, magicBuf[:]); err != nil {
		return fmt.Errorf("mongo verify read magic: %w", err)
	}
	if got := binary.LittleEndian.Uint32(magicBuf[:]); got != mongoArchiveMagic {
		return fmt.Errorf("mongo archive magic mismatch: got %#x, want %#x", got, mongoArchiveMagic)
	}

	// Drain the rest — any gzip CRC/ISIZE failure surfaces here, which is
	// exactly how we detect a truncated dump.
	if _, err := io.Copy(io.Discard, gr); err != nil {
		return fmt.Errorf("mongo verify stream: %w", err)
	}
	m.log.Debug("mongo content verified", "path", gzPath)
	return nil
}
