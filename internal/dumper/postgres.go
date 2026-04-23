package dumper

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// Postgres is the pg_dump / pg_dumpall Strategy.
type Postgres struct {
	cfg *config.Config
	log *slog.Logger
}

func init() {
	Register(config.DBPostgres, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewPostgres(cfg, log)
	})
}

func NewPostgres(cfg *config.Config, log *slog.Logger) *Postgres {
	return &Postgres{cfg: cfg, log: log}
}

func (p *Postgres) Dump(ctx context.Context) (*Artifact, error) {
	const ext = "sql"
	out := dumpFilename(p.cfg.WorkDir, ext, time.Now())

	b := NewArgBuilder().
		Add("-h", p.cfg.DB.Host).
		Add("-p", strconv.Itoa(p.cfg.DB.Port)).
		Add("-U", p.cfg.DB.User).
		AddRaw(p.cfg.DB.DumpOptions)

	cmdName := "pg_dumpall"
	if p.cfg.DB.Name != "" {
		cmdName = "pg_dump"
		b.Add(p.cfg.DB.Name)
	}
	args := b.Build()

	p.log.Info("executing postgres dump", "cmd", cmdName, "args", args, "out", out)

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+p.cfg.DB.Password)

	return runDumpWithGzip(cmd, out, ext)
}
