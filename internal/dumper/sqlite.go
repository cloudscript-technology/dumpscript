package dumper

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBSQLite, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewSQLite(cfg, log)
	})
}

// SQLite dumps a local database file via `sqlite3 <path> .dump`. The path is
// supplied as DB_NAME (SQLite has no host/port/user model). Output is plain
// SQL starting with `BEGIN TRANSACTION;` and ending with `COMMIT;`.
type SQLite struct {
	cfg *config.Config
	log *slog.Logger
}

func NewSQLite(cfg *config.Config, log *slog.Logger) *SQLite { return &SQLite{cfg: cfg, log: log} }

func (s *SQLite) Dump(ctx context.Context) (*Artifact, error) {
	if s.cfg.DB.Name == "" {
		return nil, fmt.Errorf("sqlite dump: DB_NAME (path to .sqlite file) is required")
	}
	const ext = "sql"
	out := dumpFilename(s.cfg.WorkDir, ext, time.Now())

	b := NewArgBuilder().Add(s.cfg.DB.Name)
	b.AddRaw(s.cfg.DB.DumpOptions)
	b.Add(".dump")

	s.log.Info("executing sqlite3 .dump", "db_file", s.cfg.DB.Name, "out", out)

	cmd := exec.CommandContext(ctx, "sqlite3", b.Build()...)
	return runDumpWithGzip(cmd, out, ext)
}
