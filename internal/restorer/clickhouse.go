package restorer

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBClickhouse, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewClickhouse(cfg, log)
	})
}

// Clickhouse restores a single-table Native dump via
// `clickhouse-client --query="INSERT INTO <db>.<table> FORMAT Native"` with
// stdin = decompressed dump bytes. The target table must already exist with
// a matching schema: Native preserves neither DDL nor column types.
type Clickhouse struct {
	cfg *config.Config
	log *slog.Logger
}

func NewClickhouse(cfg *config.Config, log *slog.Logger) *Clickhouse {
	return &Clickhouse{cfg: cfg, log: log}
}

func (c *Clickhouse) Restore(ctx context.Context, gzPath string) error {
	if !strings.Contains(c.cfg.DB.Name, ".") {
		return fmt.Errorf("clickhouse restore: DB_NAME must be '<database>.<table>' (got %q)", c.cfg.DB.Name)
	}
	q := fmt.Sprintf("INSERT INTO %s FORMAT Native", c.cfg.DB.Name)
	args := []string{
		"--host", c.cfg.DB.Host,
		"--port", strconv.Itoa(c.cfg.DB.Port),
	}
	if c.cfg.DB.User != "" {
		args = append(args, "--user", c.cfg.DB.User)
	}
	if c.cfg.DB.Password != "" {
		args = append(args, "--password", c.cfg.DB.Password)
	}
	args = append(args, "--query", q)

	c.log.Info("executing clickhouse-client INSERT FORMAT Native",
		"host", c.cfg.DB.Host, "port", c.cfg.DB.Port, "target", c.cfg.DB.Name, "src", gzPath)

	cmd := exec.CommandContext(ctx, "clickhouse-client", args...)
	return streamGzipToStdin(cmd, gzPath)
}
