package restorer

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// Postgres restores using psql.
type Postgres struct {
	cfg *config.Config
	log *slog.Logger
}

func init() {
	Register(config.DBPostgres, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewPostgres(cfg, log)
	})
}

func NewPostgres(cfg *config.Config, log *slog.Logger) *Postgres {
	return &Postgres{cfg: cfg, log: log}
}

func (p *Postgres) Restore(ctx context.Context, gzPath string) error {
	if p.cfg.DB.CreateDB && p.cfg.DB.Name != "" {
		if err := p.createDatabase(ctx); err != nil {
			p.log.Warn("create database failed (may already exist)", "err", err)
		}
	}

	target := p.cfg.DB.Name
	if target == "" {
		target = "postgres"
	}

	args := []string{
		"-h", p.cfg.DB.Host,
		"-p", strconv.Itoa(p.cfg.DB.Port),
		"-U", p.cfg.DB.User,
		"-d", target,
	}
	p.log.Info("executing psql restore", "args", args, "src", gzPath)

	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+p.cfg.DB.Password)
	return streamGzipToStdin(cmd, gzPath)
}

func (p *Postgres) createDatabase(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "psql",
		"-h", p.cfg.DB.Host,
		"-p", strconv.Itoa(p.cfg.DB.Port),
		"-U", p.cfg.DB.User,
		"-d", "postgres",
		"-c", `CREATE DATABASE "`+p.cfg.DB.Name+`";`,
	)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+p.cfg.DB.Password)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
