package verifier

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBMySQL, func(log *slog.Logger) Verifier { return NewMySQL(log) })
	Register(config.DBMariaDB, func(log *slog.Logger) Verifier { return NewMariaDB(log) })
}

// mysqlFooter is the marker mysqldump and mariadb-dump both emit at the end
// of a successful dump (unless --skip-comments is set).
var mysqlFooter = []byte("-- Dump completed")

// SQLFooter verifies MySQL/MariaDB dumps by looking for the canonical
// "-- Dump completed [on ...]" comment in the file tail.
//
// Caveat: if the user passes --skip-comments in DUMP_OPTIONS the footer will
// not be present and verification will reject otherwise-valid dumps. In that
// case set VERIFY_CONTENT=false.
type SQLFooter struct {
	engine string
	log    *slog.Logger
}

// NewMySQL returns a SQLFooter verifier tagged for MySQL.
func NewMySQL(log *slog.Logger) *SQLFooter {
	return &SQLFooter{engine: "mysql", log: log}
}

// NewMariaDB returns a SQLFooter verifier tagged for MariaDB.
func NewMariaDB(log *slog.Logger) *SQLFooter {
	return &SQLFooter{engine: "mariadb", log: log}
}

func (s *SQLFooter) Verify(_ context.Context, gzPath string) error {
	tail, err := streamGzipAndTail(gzPath, 4096)
	if err != nil {
		return fmt.Errorf("%s verify: %w", s.engine, err)
	}
	if !bytes.Contains(tail, mysqlFooter) {
		return fmt.Errorf("%s dump footer (-- Dump completed) missing; dump is likely truncated (or DUMP_OPTIONS contains --skip-comments)", s.engine)
	}
	s.log.Debug("sql content verified", "engine", s.engine, "path", gzPath)
	return nil
}
