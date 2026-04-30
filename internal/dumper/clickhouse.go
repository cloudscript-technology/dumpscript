package dumper

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBClickhouse, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewClickhouse(cfg, log)
	})
}

// Clickhouse dumps a single table via `clickhouse-client ... --query="SELECT *
// FROM <db>.<table> FORMAT Native"`. DB_NAME must be of the form
// `database.table`; for multi-table dumps schedule separate runs.
// FORMAT Native is the compact columnar binary interchange format and
// round-trips cleanly through clickhouse-client on restore.
type Clickhouse struct {
	cfg *config.Config
	log *slog.Logger
}

func NewClickhouse(cfg *config.Config, log *slog.Logger) *Clickhouse {
	return &Clickhouse{cfg: cfg, log: log}
}

func (c *Clickhouse) Dump(ctx context.Context) (*Artifact, error) {
	if !strings.Contains(c.cfg.DB.Name, ".") {
		return nil, fmt.Errorf("clickhouse dump: DB_NAME must be '<database>.<table>' (got %q)", c.cfg.DB.Name)
	}
	const ext = "native"
	out := dumpFilename(c.cfg.WorkDir, ext, time.Now())
	q := fmt.Sprintf("SELECT * FROM %s FORMAT Native", c.cfg.DB.Name)

	b := NewArgBuilder().
		Add("--host", c.cfg.DB.Host).
		Add("--port", strconv.Itoa(c.cfg.DB.Port))
	if c.cfg.DB.User != "" {
		b.Add("--user", c.cfg.DB.User)
	}
	// Pass password via env (CLICKHOUSE_PASSWORD) instead of --password argv
	// so it does not show up in /proc/PID/cmdline or `ps`.
	b.AddRaw(c.cfg.DB.DumpOptions)
	b.Add("--query", q)

	c.log.Info("executing clickhouse-client FORMAT Native",
		"host", c.cfg.DB.Host, "port", c.cfg.DB.Port, "target", c.cfg.DB.Name, "out", out)

	cmd := exec.CommandContext(ctx, "clickhouse-client", b.Build()...)
	if c.cfg.DB.Password != "" {
		cmd.Env = append(os.Environ(), "CLICKHOUSE_PASSWORD="+c.cfg.DB.Password)
	}
	return runDumpWithGzip(cmd, out, ext)
}
