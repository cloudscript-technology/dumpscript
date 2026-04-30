package restorer

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBCockroach, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewCockroach(cfg, log)
	})
}

// Cockroach restores using psql (CockroachDB speaks the Postgres wire protocol).
type Cockroach struct {
	cfg *config.Config
	log *slog.Logger
}

func NewCockroach(cfg *config.Config, log *slog.Logger) *Cockroach {
	return &Cockroach{cfg: cfg, log: log}
}

func (c *Cockroach) Restore(ctx context.Context, gzPath string) error {
	target := c.cfg.DB.Name
	if target == "" {
		target = "defaultdb"
	}
	args := []string{
		"-h", c.cfg.DB.Host,
		"-p", strconv.Itoa(c.cfg.DB.Port),
		"-U", c.cfg.DB.User,
		"-d", target,
	}
	c.log.Info("executing cockroach restore (psql)", "args", args, "src", gzPath)

	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+c.cfg.DB.Password)
	return streamGzipToStdin(cmd, gzPath)
}
