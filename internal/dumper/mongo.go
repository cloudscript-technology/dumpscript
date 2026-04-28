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

// Mongo is the mongodump Strategy. It uses --archive --gzip so mongodump
// writes a gzipped archive directly to stdout; we just pipe it to the file.
type Mongo struct {
	cfg *config.Config
	log *slog.Logger
}

func init() {
	Register(config.DBMongo, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewMongo(cfg, log)
	})
}

func NewMongo(cfg *config.Config, log *slog.Logger) *Mongo {
	return &Mongo{cfg: cfg, log: log}
}

func (m *Mongo) Dump(ctx context.Context) (*Artifact, error) {
	const ext = "archive"
	out := dumpFilename(m.cfg.WorkDir, ext, time.Now())

	b := NewArgBuilder().
		AddRaw(m.cfg.DB.DumpOptions).
		Add("--host", m.cfg.DB.Host).
		Add("--port", strconv.Itoa(m.cfg.DB.Port)).
		Add("--archive", "--gzip")

	if m.cfg.DB.User != "" {
		b.Add("--username", m.cfg.DB.User)
	}
	// Password is passed via a temporary --config file (mongo-tools 100.7+
	// feature) instead of --password argv so it doesn't appear in
	// /proc/PID/cmdline or `ps`. Cleanup is best-effort via defer.
	cfgPath, cleanup, err := writeMongoCredsFile(m.cfg.DB.Password)
	if err != nil {
		return nil, fmt.Errorf("write mongo creds: %w", err)
	}
	defer cleanup()
	if cfgPath != "" {
		b.Add("--config", cfgPath)
	}

	if m.cfg.DB.Name != "" {
		b.Add("--db", m.cfg.DB.Name)
	}
	args := b.Build()

	m.log.Info("executing mongodump", "host", m.cfg.DB.Host, "db", m.cfg.DB.Name, "out", out)

	cmd := exec.CommandContext(ctx, "mongodump", args...)
	return runMongoDump(cmd, out)
}

// writeMongoCredsFile writes a YAML config file containing the Mongo password
// (keyed as `password:` per mongo-tools docs) into a 0600 temp file, returning
// the path and a cleanup function. Returns ("", noop, nil) when password is
// empty so the caller can skip --config entirely.
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
	cleanup := func() { os.Remove(f.Name()) } //nolint:errcheck
	return f.Name(), cleanup, nil
}
