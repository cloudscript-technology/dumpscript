package restorer

import (
	"context"
	"errors"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBEtcd, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewEtcd(cfg, log)
	})
}

// ErrEtcdRestoreUnsupported — see type doc.
var ErrEtcdRestoreUnsupported = errors.New(
	"etcd snapshot restore is not supported by dumpscript: the operation rebuilds a " +
		"cluster from the snapshot (new --initial-cluster-token, new data dir) and must " +
		"be coordinated across every member. Run `etcdctl snapshot restore <file> " +
		"--data-dir=<new>` on each node manually after downloading the artifact")

// Etcd restore is intentionally unimplemented: `etcdctl snapshot restore`
// produces a fresh data-dir that must be bootstrapped as a new cluster, which
// requires coordination beyond what a dump tool can perform safely.
type Etcd struct {
	cfg *config.Config
	log *slog.Logger
}

func NewEtcd(cfg *config.Config, log *slog.Logger) *Etcd { return &Etcd{cfg: cfg, log: log} }

func (e *Etcd) Restore(_ context.Context, gzPath string) error {
	e.log.Error("etcd restore requested but not supported", "downloaded_path", gzPath)
	return ErrEtcdRestoreUnsupported
}
