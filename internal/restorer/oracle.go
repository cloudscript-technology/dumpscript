package restorer

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBOracle, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewOracle(cfg, log)
	})
}

// Oracle restores via the legacy `imp` Data Pump import tool. DB_NAME is
// the Oracle service name (EZCONNECT format).
//
// The .dmp is binary — the existing streamGzipToStdin helper decompresses
// the gzip wrapper and feeds the raw bytes to imp; it doesn't care that the
// stream isn't text.
//
// Requires Oracle Instant Client `imp` on PATH (not bundled by default).
type Oracle struct {
	cfg *config.Config
	log *slog.Logger
}

func NewOracle(cfg *config.Config, log *slog.Logger) *Oracle {
	return &Oracle{cfg: cfg, log: log}
}

func (o *Oracle) Restore(ctx context.Context, gzPath string) error {
	if o.cfg.DB.Name == "" {
		return fmt.Errorf("oracle restore: DB_NAME (service name) is required")
	}
	connect := fmt.Sprintf("%s/%s@%s:%d/%s",
		o.cfg.DB.User, o.cfg.DB.Password, o.cfg.DB.Host, o.cfg.DB.Port, o.cfg.DB.Name)

	args := []string{connect, "FULL=Y", "FILE=/dev/stdin"}
	o.log.Info("executing Oracle imp",
		"host", o.cfg.DB.Host, "service", o.cfg.DB.Name, "src", gzPath)

	cmd := exec.CommandContext(ctx, "imp", args...)
	return streamGzipToStdin(cmd, gzPath)
}
