package restorer

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBRedis, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewRedis(cfg, log)
	})
}

// ErrRedisRestoreUnsupported is returned by Redis.Restore — see type doc.
var ErrRedisRestoreUnsupported = errors.New(
	"redis RDB restore is not supported by dumpscript: stop redis-server, " +
		"replace <redis-data-dir>/dump.rdb with the downloaded file, then restart. " +
		"For online/key-level restores use a dedicated tool such as redis-dump-go or RIOT")

// Redis restore is intentionally unimplemented: the RDB snapshot format must
// be loaded by the server at startup, which requires filesystem access to
// the Redis data directory and a server restart — outside the scope of a
// dump tool. The Restorer interface is still satisfied so the engine shows
// up in registry diagnostics and the restore CLI returns a helpful error
// instead of a confusing lookup failure.
type Redis struct {
	cfg *config.Config
	log *slog.Logger
}

func NewRedis(cfg *config.Config, log *slog.Logger) *Redis {
	return &Redis{cfg: cfg, log: log}
}

func (r *Redis) Restore(_ context.Context, gzPath string) error {
	r.log.Error("redis restore requested but not supported", "downloaded_path", gzPath)
	return ErrRedisRestoreUnsupported
}
