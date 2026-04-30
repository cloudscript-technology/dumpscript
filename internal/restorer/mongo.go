package restorer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
		"--archive", "--gzip",
	)
	if m.cfg.DB.User != "" {
		args = append(args, "--username", m.cfg.DB.User)
	}
	// Password via temp --config YAML (see internal/dumper/mongo.go) so it
	// doesn't show up in /proc/PID/cmdline.
	cfgPath, cleanup, err := writeMongoCredsFile(m.cfg.DB.Password)
	if err != nil {
		return fmt.Errorf("write mongo creds: %w", err)
	}
	defer cleanup()
	if cfgPath != "" {
		args = append(args, "--config", cfgPath)
	}
	if m.cfg.DB.Name != "" {
		args = append(args, "--db", m.cfg.DB.Name)
	}

	m.log.Info("executing mongorestore", "host", m.cfg.DB.Host, "db", m.cfg.DB.Name, "src", gzPath)
	cmd := exec.CommandContext(ctx, "mongorestore", args...)
	return streamRawToStdin(cmd, gzPath)
}

// writeMongoCredsFile mirrors the dumper-side helper. Duplicated here to keep
// internal/restorer free of an internal/dumper dependency.
func writeMongoCredsFile(password string) (string, func(), error) {
	noop := func() {}
	if password == "" {
		return "", noop, nil
	}
	f, err := os.CreateTemp("", "mongocfg-*.yaml")
	if err != nil {
		return "", noop, err
	}
	if err := os.Chmod(f.Name(), 0o600); err != nil {
		f.Close() //nolint:errcheck
		os.Remove(f.Name()) //nolint:errcheck
		return "", noop, err
	}
	if _, err := fmt.Fprintf(f, "password: %q\n", password); err != nil {
		f.Close() //nolint:errcheck
		os.Remove(f.Name()) //nolint:errcheck
		return "", noop, err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name()) //nolint:errcheck
		return "", noop, err
	}
	return f.Name(), func() { os.Remove(f.Name()) }, nil //nolint:errcheck
}
