package verifier

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// sqlserverFooter is the T-SQL batch terminator that mssql-scripter emits
// after every statement. A complete dump ends with `\nGO\n` (possibly
// followed by whitespace). A SIGKILL mid-statement leaves the tail without
// a trailing `\nGO` even if earlier `GO` lines are present — we verify the
// *trailing* terminator, not just its presence anywhere.
var sqlserverFooter = []byte("\nGO")

func init() {
	Register(config.DBSQLServer, func(log *slog.Logger) Verifier { return NewSQLServer(log) })
}

type SQLServer struct {
	log *slog.Logger
}

func NewSQLServer(log *slog.Logger) *SQLServer { return &SQLServer{log: log} }

func (s *SQLServer) Verify(_ context.Context, gzPath string) error {
	tail, err := streamGzipAndTail(gzPath, 4096)
	if err != nil {
		return fmt.Errorf("sqlserver verify: %w", err)
	}
	trimmed := bytes.TrimRight(tail, " \t\r\n")
	if !bytes.HasSuffix(trimmed, sqlserverFooter) {
		return fmt.Errorf("sqlserver dump does not end with a GO batch terminator; dump likely truncated")
	}
	s.log.Debug("sqlserver content verified", "path", gzPath)
	return nil
}
