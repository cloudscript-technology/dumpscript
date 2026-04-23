package verifier

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// sqliteFooter is the final statement emitted by `sqlite3 <db> .dump`. A
// SIGKILL mid-dump leaves a truncated INSERT without reaching COMMIT.
var sqliteFooter = []byte("COMMIT;")

func init() {
	Register(config.DBSQLite, func(log *slog.Logger) Verifier { return NewSQLite(log) })
}

type SQLite struct {
	log *slog.Logger
}

func NewSQLite(log *slog.Logger) *SQLite { return &SQLite{log: log} }

func (s *SQLite) Verify(_ context.Context, gzPath string) error {
	tail, err := streamGzipAndTail(gzPath, 4096)
	if err != nil {
		return fmt.Errorf("sqlite verify: %w", err)
	}
	trimmed := bytes.TrimRight(tail, " \t\r\n")
	if !bytes.HasSuffix(trimmed, sqliteFooter) {
		return fmt.Errorf("sqlite dump does not end with COMMIT; dump likely truncated")
	}
	s.log.Debug("sqlite content verified", "path", gzPath)
	return nil
}
