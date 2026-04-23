package restorer

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBNeo4j, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewNeo4j(cfg, log)
	})
}

// Neo4j restores via `neo4j-admin database load --from-stdin` (Neo4j 5+).
// The target database must be stopped — the tool writes store files directly.
type Neo4j struct {
	cfg *config.Config
	log *slog.Logger
}

func NewNeo4j(cfg *config.Config, log *slog.Logger) *Neo4j { return &Neo4j{cfg: cfg, log: log} }

func (n *Neo4j) Restore(ctx context.Context, gzPath string) error {
	if n.cfg.DB.Name == "" {
		return fmt.Errorf("neo4j restore: DB_NAME (database) is required")
	}
	args := []string{
		"database", "load",
		"--from-stdin",
		"--database=" + n.cfg.DB.Name,
		"--overwrite-destination=true",
	}
	n.log.Info("executing neo4j-admin database load",
		"database", n.cfg.DB.Name, "src", gzPath)
	cmd := exec.CommandContext(ctx, "neo4j-admin", args...)
	return streamGzipToStdin(cmd, gzPath)
}
