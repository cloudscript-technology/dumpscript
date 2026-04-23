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
	Register(config.DBNeo4j, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewNeo4j(cfg, log)
	})
}

// Neo4j dumps a single database via `neo4j-admin database dump --to-stdout`
// (Neo4j 5+). The tool reads store files directly, so Community Edition
// requires the database stopped. Online hot backups need Neo4j Enterprise +
// `neo4j-admin backup` — out of scope for this tool.
type Neo4j struct {
	cfg *config.Config
	log *slog.Logger
}

func NewNeo4j(cfg *config.Config, log *slog.Logger) *Neo4j { return &Neo4j{cfg: cfg, log: log} }

func (n *Neo4j) Dump(ctx context.Context) (*Artifact, error) {
	if n.cfg.DB.Name == "" {
		return nil, fmt.Errorf("neo4j dump: DB_NAME (database) is required")
	}
	const ext = "neo4j"
	out := dumpFilename(n.cfg.WorkDir, ext, time.Now())

	b := NewArgBuilder().
		Add("database", "dump", "--to-stdout").
		Add("--database=" + n.cfg.DB.Name)
	b.AddRaw(n.cfg.DB.DumpOptions)

	n.log.Info("executing neo4j-admin database dump",
		"database", n.cfg.DB.Name, "out", out)

	cmd := exec.CommandContext(ctx, "neo4j-admin", b.Build()...)
	return runDumpWithGzip(cmd, out, ext)
}
