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
	Register(config.DBSQLServer, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewSQLServer(cfg, log)
	})
}

// SQLServer dumps via `mssql-scripter`, Microsoft's official schema+data SQL
// script generator (https://github.com/microsoft/mssql-scripter).
//
// The resulting SQL is streamed on stdout, gzipped and saved. Requires
// `mssql-scripter` on PATH — install with `pip install mssql-scripter`
// (not bundled in the default Alpine image).
type SQLServer struct {
	cfg *config.Config
	log *slog.Logger
}

func NewSQLServer(cfg *config.Config, log *slog.Logger) *SQLServer {
	return &SQLServer{cfg: cfg, log: log}
}

func (s *SQLServer) Dump(ctx context.Context) (*Artifact, error) {
	const ext = "sql"
	out := dumpFilename(s.cfg.WorkDir, ext, time.Now())

	if s.cfg.DB.Name == "" {
		return nil, fmt.Errorf("sqlserver dump: DB_NAME is required (mssql-scripter has no --all-databases)")
	}

	server := fmt.Sprintf("%s,%d", s.cfg.DB.Host, s.cfg.DB.Port)
	// SECURITY NOTE: mssql-scripter does not support reading the password from
	// an environment variable or config file, so it has to be passed via argv
	// (-P). On a shared host this leaks via /proc/PID/cmdline. Recommended
	// mitigation in production is to use SQL Server Azure AD/Managed Identity
	// auth (out of scope for this dumper today).
	args := NewArgBuilder().
		Add("-S", server).
		Add("-d", s.cfg.DB.Name).
		Add("-U", s.cfg.DB.User).
		Add("-P", s.cfg.DB.Password).
		Add("--script-compatibility-option", "SQLServer2019").
		AddRaw(s.cfg.DB.DumpOptions).
		Build()

	s.log.Info("executing mssql-scripter",
		"server", server, "db", s.cfg.DB.Name, "out", out)

	cmd := exec.CommandContext(ctx, "mssql-scripter", args...)
	return runDumpWithGzip(cmd, out, ext)
}
