package dumper

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// MySQL is the Strategy for MySQL/MariaDB-compatible dumps. It prefers
// `mysqldump` when present and transparently falls back to `mariadb-dump` —
// which is a drop-in replacement shipped by the MariaDB client package and
// covers MySQL 5.7/8.0 as well as MariaDB 10.x/11.x. The `MYSQL_VERSION`
// config is kept for backward compat but no longer changes behavior.
type MySQL struct {
	cfg *config.Config
	log *slog.Logger
}

func init() {
	Register(config.DBMySQL, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewMySQL(cfg, log)
	})
}

func NewMySQL(cfg *config.Config, log *slog.Logger) *MySQL {
	return &MySQL{cfg: cfg, log: log}
}

func (m *MySQL) Dump(ctx context.Context) (*Artifact, error) {
	const ext = "sql"
	out := dumpFilename(m.cfg.WorkDir, ext, time.Now())

	cmdName, err := m.resolveCommand()
	if err != nil {
		return nil, err
	}

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

	m.log.Info("executing mysql dump", "cmd", cmdName, "args", args, "out", out)

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Env = append(os.Environ(), "MYSQL_PWD="+m.cfg.DB.Password)

	return runDumpWithGzip(cmd, out, ext)
}

// resolveCommand picks the dump binary: prefers mysqldump when available,
// otherwise uses mariadb-dump (drop-in replacement covering every supported
// server version).
func (m *MySQL) resolveCommand() (string, error) {
	if _, err := exec.LookPath("mysqldump"); err == nil {
		return "mysqldump", nil
	}
	if _, err := exec.LookPath("mariadb-dump"); err == nil {
		return "mariadb-dump", nil
	}
	return "", fmt.Errorf("neither mysqldump nor mariadb-dump found on PATH (install mariadb-client)")
}
