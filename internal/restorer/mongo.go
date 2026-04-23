package restorer

import (
	"context"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// Mongo restores using mongorestore --archive --gzip on stdin.
type Mongo struct {
	cfg *config.Config
	log *slog.Logger
}

func init() {
	Register(config.DBMongo, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewMongo(cfg, log)
	})
}

func NewMongo(cfg *config.Config, log *slog.Logger) *Mongo {
	return &Mongo{cfg: cfg, log: log}
}

func (m *Mongo) Restore(ctx context.Context, gzPath string) error {
	args := []string{}
	// Honor DUMP_OPTIONS (e.g., --authenticationDatabase=admin) so the same
	// auth arguments used for mongodump also apply to mongorestore.
	for _, a := range strings.Fields(m.cfg.DB.DumpOptions) {
		args = append(args, a)
	}
	args = append(args,
		"--host", m.cfg.DB.Host,
		"--port", strconv.Itoa(m.cfg.DB.Port),
		"--username", m.cfg.DB.User,
		"--password", m.cfg.DB.Password,
		"--archive", "--gzip",
	)
	if m.cfg.DB.Name != "" {
		args = append(args, "--db", m.cfg.DB.Name)
	}

	m.log.Info("executing mongorestore", "host", m.cfg.DB.Host, "db", m.cfg.DB.Name, "src", gzPath)
	cmd := exec.CommandContext(ctx, "mongorestore", args...)
	return streamRawToStdin(cmd, gzPath)
}
