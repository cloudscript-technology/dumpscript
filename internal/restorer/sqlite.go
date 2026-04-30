package restorer

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBSQLite, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewSQLite(cfg, log)
	})
}

// SQLite restores via `sqlite3 <path>` reading plain SQL (BEGIN/COMMIT) from
// stdin. The target file is created if missing — sqlite3 default behavior.
type SQLite struct {
	cfg *config.Config
	log *slog.Logger
}

func NewSQLite(cfg *config.Config, log *slog.Logger) *SQLite { return &SQLite{cfg: cfg, log: log} }

func (s *SQLite) Restore(ctx context.Context, gzPath string) error {
	if s.cfg.DB.Name == "" {
		return fmt.Errorf("sqlite restore: DB_NAME (path to .sqlite file) is required")
	}
	s.log.Info("executing sqlite3 restore", "db_file", s.cfg.DB.Name, "src", gzPath)
	cmd := exec.CommandContext(ctx, "sqlite3", s.cfg.DB.Name)
	return streamGzipToStdin(cmd, gzPath)
}
