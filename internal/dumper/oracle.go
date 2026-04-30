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
	Register(config.DBOracle, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewOracle(cfg, log)
	})
}

// Oracle dumps via the legacy `exp` Data Pump client. We pick `exp` over
// `expdp` because `expdp` writes to a server-side DATA_PUMP_DIR (requires a
// shared filesystem with dumpscript); `exp` writes to the client stdout so
// we can stream it straight into gzip.
//
// DB_NAME is interpreted as the Oracle service name (e.g. XEPDB1, ORCLCDB).
//
// Requires the Oracle Instant Client `exp` binary on PATH (not bundled —
// Oracle has no official Alpine package; install via `instantclient-tools`
// or switch to a Debian-based base image for production use).
type Oracle struct {
	cfg *config.Config
	log *slog.Logger
}

func NewOracle(cfg *config.Config, log *slog.Logger) *Oracle {
	return &Oracle{cfg: cfg, log: log}
}

func (o *Oracle) Dump(ctx context.Context) (*Artifact, error) {
	const ext = "dmp"
	out := dumpFilename(o.cfg.WorkDir, ext, time.Now())

	if o.cfg.DB.Name == "" {
		return nil, fmt.Errorf("oracle dump: DB_NAME (service name) is required")
	}

	// Oracle EZCONNECT: user/pass@host:port/service
	connect := fmt.Sprintf("%s/%s@%s:%d/%s",
		o.cfg.DB.User, o.cfg.DB.Password, o.cfg.DB.Host, o.cfg.DB.Port, o.cfg.DB.Name)

	b := NewArgBuilder().
		Add("FULL=Y").           // schema-wide dump by default
		Add("CONSISTENT=Y").     // read-consistent snapshot
		Add("FILE=/dev/stdout"). // stream to our stdout
		AddRaw(o.cfg.DB.DumpOptions)

	args := append([]string{connect}, b.Build()...)

	o.log.Info("executing Oracle exp",
		"host", o.cfg.DB.Host, "service", o.cfg.DB.Name, "out", out)

	cmd := exec.CommandContext(ctx, "exp", args...)
	return runDumpWithGzip(cmd, out, ext)
}
