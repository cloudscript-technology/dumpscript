package dumper

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBEtcd, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewEtcd(cfg, log)
	})
}

// Etcd takes a cluster snapshot with `etcdctl snapshot save`. The tool does
// not accept stdout as a target (it uses atomic rename), so we write to a
// tmp file and stream it through gzip.
//
// Scheme: http unless DUMP_OPTIONS contains --scheme=https.
type Etcd struct {
	cfg *config.Config
	log *slog.Logger
}

func NewEtcd(cfg *config.Config, log *slog.Logger) *Etcd { return &Etcd{cfg: cfg, log: log} }

func (e *Etcd) Dump(ctx context.Context) (*Artifact, error) {
	const ext = "db"
	out := dumpFilename(e.cfg.WorkDir, ext, time.Now())
	scheme := "http"
	if strings.Contains(e.cfg.DB.DumpOptions, "--scheme=https") {
		scheme = "https"
	}
	endpoint := fmt.Sprintf("%s://%s:%d", scheme, e.cfg.DB.Host, e.cfg.DB.Port)
	e.log.Info("executing etcdctl snapshot save", "endpoint", endpoint, "out", out)

	return runDumpViaTempFile(func(tmpPath string) error {
		_ = os.Remove(tmpPath) // etcdctl refuses to overwrite existing files.
		args := NewArgBuilder().Add("--endpoints", endpoint)
		if e.cfg.DB.User != "" {
			creds := e.cfg.DB.User
			if e.cfg.DB.Password != "" {
				creds += ":" + e.cfg.DB.Password
			}
			args.Add("--user", creds)
		}
		args.AddRaw(stripEtcdConsumedOptions(e.cfg.DB.DumpOptions))
		args.Add("snapshot", "save", tmpPath)

		cmd := exec.CommandContext(ctx, "etcdctl", args.Build()...)
		cmd.Env = append(os.Environ(), "ETCDCTL_API=3")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}, out, ext)
}

// stripEtcdConsumedOptions removes tokens the dumper itself interprets
// (`--scheme=...`) so they don't leak into etcdctl's argv.
func stripEtcdConsumedOptions(raw string) string {
	var kept []string
	for _, tok := range strings.Fields(raw) {
		if strings.HasPrefix(tok, "--scheme=") {
			continue
		}
		kept = append(kept, tok)
	}
	return strings.Join(kept, " ")
}
