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

func init() {
	Register(config.DBRedis, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewRedis(cfg, log)
	})
}

// Redis streams an RDB snapshot from a live server using `redis-cli --rdb -`.
// The binary RDB is piped into a gzip.Writer → file. Auth uses `-a <password>`
// and optionally `--user` for ACL-based auth (Redis 6+).
type Redis struct {
	cfg *config.Config
	log *slog.Logger
}

func NewRedis(cfg *config.Config, log *slog.Logger) *Redis {
	return &Redis{cfg: cfg, log: log}
}

func (r *Redis) Dump(ctx context.Context) (*Artifact, error) {
	const ext = "rdb"
	out := dumpFilename(r.cfg.WorkDir, ext, time.Now())

	r.log.Info("executing redis-cli --rdb",
		"host", r.cfg.DB.Host, "port", r.cfg.DB.Port, "out", out)

	// redis-cli --rdb calls ftruncate()/fsync() on the output which fails on
	// pipes (Invalid argument). Write to a real temp file, then stream it
	// through gzip — mirrors the etcd approach.
	return runDumpViaTempFile(func(tmpPath string) error {
		b := NewArgBuilder().
			Add("-h", r.cfg.DB.Host).
			Add("-p", strconv.Itoa(r.cfg.DB.Port))
		if r.cfg.DB.User != "" {
			b.Add("--user", r.cfg.DB.User)
		}
		if r.cfg.DB.Password != "" {
			b.Add("-a", r.cfg.DB.Password).Add("--no-auth-warning")
		}
		b.AddRaw(r.cfg.DB.DumpOptions)
		b.Add("--rdb", tmpPath)

		cmd := exec.CommandContext(ctx, "redis-cli", b.Build()...)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}, out, ext)
}
