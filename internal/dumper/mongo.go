package dumper

import (
	"context"
	"log/slog"
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
		Add("--username", m.cfg.DB.User).
		Add("--password", m.cfg.DB.Password).
		Add("--archive", "--gzip")

	if m.cfg.DB.Name != "" {
		b.Add("--db", m.cfg.DB.Name)
	}
	args := b.Build()

	m.log.Info("executing mongodump", "host", m.cfg.DB.Host, "db", m.cfg.DB.Name, "out", out)

	cmd := exec.CommandContext(ctx, "mongodump", args...)
	return runMongoDump(cmd, out)
}
