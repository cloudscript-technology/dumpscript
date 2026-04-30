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

// MariaDB is the mariadb-dump Strategy.
type MariaDB struct {
	cfg *config.Config
	log *slog.Logger
}

func init() {
	Register(config.DBMariaDB, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewMariaDB(cfg, log)
	})
}

func NewMariaDB(cfg *config.Config, log *slog.Logger) *MariaDB {
	return &MariaDB{cfg: cfg, log: log}
}

func (m *MariaDB) Dump(ctx context.Context) (*Artifact, error) {
	const ext = "sql"
	out := dumpFilename(m.cfg.WorkDir, ext, time.Now())

	b := NewArgBuilder().
		AddRaw(m.cfg.DB.DumpOptions).
		Add("-h", m.cfg.DB.Host).
		Add("-P", strconv.Itoa(m.cfg.DB.Port)).
		Add("-u", m.cfg.DB.User)

	if m.cfg.DB.Name == "" {
		b.Add("--all-databases")
	} else {
		b.Add(m.cfg.DB.Name)
	}
	args := b.Build()

	m.log.Info("executing mariadb dump", "args", args, "out", out)

	cmd := exec.CommandContext(ctx, "mariadb-dump", args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+m.cfg.DB.Password)

	return runDumpWithGzip(cmd, out, ext)
}
