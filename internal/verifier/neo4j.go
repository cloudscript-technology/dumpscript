package verifier

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBNeo4j, func(log *slog.Logger) Verifier { return NewNeo4j(log) })
}

// Neo4j 5.x archive format is not publicly documented byte-for-byte and
// `neo4j-admin database dump` already compresses the payload internally —
// attempting a magic-byte check risks false negatives across minor versions.
// The verifier therefore asserts only envelope integrity: a full gzip
// CRC/ISIZE drain plus a non-empty decompressed output.
type Neo4j struct {
	log *slog.Logger
}

func NewNeo4j(log *slog.Logger) *Neo4j { return &Neo4j{log: log} }

func (n *Neo4j) Verify(_ context.Context, gzPath string) error {
	tail, err := streamGzipAndTail(gzPath, 4096)
	if err != nil {
		return fmt.Errorf("neo4j verify: %w", err)
	}
	if len(tail) == 0 {
		return fmt.Errorf("neo4j dump is empty")
	}
	n.log.Debug("neo4j content verified", "path", gzPath, "tail_bytes", len(tail))
	return nil
}
