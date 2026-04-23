package verifier

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

var (
	pgSingleDBFooter = []byte("PostgreSQL database dump complete")
	pgClusterFooter  = []byte("PostgreSQL database cluster dump complete")
)

// Postgres verifies a pg_dump / pg_dumpall plain-SQL artifact by scanning the
// tail for the well-known "dump complete" comment. A truncated dump (the
// process got SIGKILL mid-way) produces a valid gzip but lacks the footer.
type Postgres struct {
	log *slog.Logger
}

func init() {
	ctor := func(log *slog.Logger) Verifier { return NewPostgres(log) }
	Register(config.DBPostgres, ctor)
	// CockroachDB uses pg_dump under the hood — same footer markers apply.
	Register(config.DBCockroach, ctor)
}

func NewPostgres(log *slog.Logger) *Postgres { return &Postgres{log: log} }

func (p *Postgres) Verify(_ context.Context, gzPath string) error {
	tail, err := streamGzipAndTail(gzPath, 4096)
	if err != nil {
		return fmt.Errorf("postgres verify: %w", err)
	}
	if !bytes.Contains(tail, pgSingleDBFooter) && !bytes.Contains(tail, pgClusterFooter) {
		return fmt.Errorf("postgres dump footer missing; dump is likely truncated")
	}
	p.log.Debug("postgres content verified", "path", gzPath)
	return nil
}
