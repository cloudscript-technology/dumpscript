package restorer

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBSQLServer, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewSQLServer(cfg, log)
	})
}

// SQLServer restores via `sqlcmd` reading the gunzipped SQL from stdin.
// Requires `sqlcmd` on PATH (part of mssql-tools18).
type SQLServer struct {
	cfg *config.Config
	log *slog.Logger
}

func NewSQLServer(cfg *config.Config, log *slog.Logger) *SQLServer {
	return &SQLServer{cfg: cfg, log: log}
}

func (s *SQLServer) Restore(ctx context.Context, gzPath string) error {
	if s.cfg.DB.Name == "" {
		return fmt.Errorf("sqlserver restore: DB_NAME is required")
	}
	server := fmt.Sprintf("%s,%d", s.cfg.DB.Host, s.cfg.DB.Port)
	args := []string{
		"-S", server,
		"-U", s.cfg.DB.User,
		"-P", s.cfg.DB.Password,
		"-d", s.cfg.DB.Name,
		"-b", // exit on error
	}
	s.log.Info("executing sqlserver restore (sqlcmd)",
		"server", server, "db", s.cfg.DB.Name, "src", gzPath)
	cmd := exec.CommandContext(ctx, "sqlcmd", args...)
	return streamGzipToStdin(cmd, gzPath)
}
