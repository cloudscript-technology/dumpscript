package dumper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBCockroach, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewCockroach(cfg, log)
	})
}

// Cockroach dumps a database via a psql-driven, CRDB-native strategy:
//
//   - Enumerate tables with `SHOW TABLES FROM <db>` (Cockroach-specific).
//   - For each table, emit its DDL via `SHOW CREATE TABLE` followed by the
//     data via `\copy ... TO STDOUT` (text-format, tab-separated).
//
// The assembled stream is a self-contained plain-SQL file that `psql` can
// replay back on any CockroachDB (or Postgres) cluster on restore.
//
// We deliberately avoid `pg_dump` here — `pg_dump` queries `pg_extension`
// columns (notably `tableoid`) that CockroachDB does not expose through its
// pg_catalog compatibility shim, which fails the whole dump.
type Cockroach struct {
	cfg *config.Config
	log *slog.Logger
}

func NewCockroach(cfg *config.Config, log *slog.Logger) *Cockroach {
	return &Cockroach{cfg: cfg, log: log}
}

func (c *Cockroach) Dump(ctx context.Context) (*Artifact, error) {
	if c.cfg.DB.Name == "" {
		return nil, fmt.Errorf("cockroach dump: DB_NAME is required")
	}
	const ext = "sql"
	out := dumpFilename(c.cfg.WorkDir, ext, time.Now())

	c.log.Info("executing cockroach dump (psql + SHOW CREATE)",
		"host", c.cfg.DB.Host, "port", c.cfg.DB.Port, "db", c.cfg.DB.Name, "out", out)

	return runNativeDump(func(w io.Writer) error {
		return c.dumpAll(ctx, w)
	}, out, ext)
}

func (c *Cockroach) dumpAll(ctx context.Context, w io.Writer) error {
	tables, err := c.listTables(ctx)
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	if _, err := fmt.Fprintf(w, "-- Cockroach dump of %s\nBEGIN;\n\n", c.cfg.DB.Name); err != nil {
		return err
	}
	for _, t := range tables {
		if err := c.dumpTable(ctx, w, t); err != nil {
			return fmt.Errorf("table %s: %w", t, err)
		}
	}
	// Closing footer. The trailing comment doubles as the verifier marker
	// (the Cockroach engine shares the Postgres verifier, which scans for
	// this exact string to confirm a non-truncated dump).
	if _, err := io.WriteString(w, "COMMIT;\n\n--\n-- PostgreSQL database dump complete\n--\n"); err != nil {
		return err
	}
	c.log.Info("cockroach dump complete", "tables", len(tables))
	return nil
}

// listTables returns unqualified table names in the public schema.
func (c *Cockroach) listTables(ctx context.Context) ([]string, error) {
	out, err := c.psqlQuery(ctx,
		`SELECT table_name FROM [SHOW TABLES FROM `+quoteIdent(c.cfg.DB.Name)+`] WHERE schema_name = 'public' ORDER BY table_name`)
	if err != nil {
		return nil, err
	}
	var tables []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			tables = append(tables, t)
		}
	}
	return tables, nil
}

// dumpTable emits CREATE TABLE + COPY FROM STDIN payload for one table.
func (c *Cockroach) dumpTable(ctx context.Context, w io.Writer, table string) error {
	// SHOW CREATE returns a two-column result: table name | create_statement.
	// We extract the second column with --tuples-only + --field-separator.
	create, err := c.psqlQueryTwoCol(ctx, `SHOW CREATE TABLE `+quoteIdent(c.cfg.DB.Name)+`.`+quoteIdent(table))
	if err != nil {
		return fmt.Errorf("show create: %w", err)
	}
	if _, err := fmt.Fprintf(w, "-- Table: %s\n%s;\n\n", table, strings.TrimRight(create, ";\n ")); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "COPY %s FROM stdin;\n", quoteIdent(table)); err != nil {
		return err
	}
	if err := c.psqlCopyOut(ctx, w, table); err != nil {
		return fmt.Errorf("copy out: %w", err)
	}
	if _, err := io.WriteString(w, "\\.\n\n"); err != nil {
		return err
	}
	return nil
}

// psqlQuery runs `psql -At -c <query>` and returns stdout.
func (c *Cockroach) psqlQuery(ctx context.Context, query string) (string, error) {
	args := c.baseArgs()
	args = append(args, "-A", "-t", "-c", query)
	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = c.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return stdout.String(), nil
}

// psqlQueryTwoCol runs `psql -At -F | -c <query>` and returns the 2nd column
// of the first row. SHOW CREATE TABLE output is multi-line (the CREATE
// statement is a single column value with embedded newlines), so we take
// everything after the first pipe byte — newlines and all — through EOF.
func (c *Cockroach) psqlQueryTwoCol(ctx context.Context, query string) (string, error) {
	args := c.baseArgs()
	args = append(args, "-A", "-t", "-F", "|", "-c", query)
	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = c.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	out := stdout.Bytes()
	i := bytes.IndexByte(out, '|')
	if i < 0 {
		return "", fmt.Errorf("psql output missing field separator: %q", bytes.TrimSpace(out))
	}
	return string(bytes.TrimRight(out[i+1:], "\n\r ")), nil
}

// psqlCopyOut runs `psql -c "\copy <table> TO STDOUT"` and writes the data
// (tab-separated text format, matching `COPY FROM stdin;`) to w.
func (c *Cockroach) psqlCopyOut(ctx context.Context, w io.Writer, table string) error {
	args := c.baseArgs()
	args = append(args, "-c", `\copy `+quoteIdent(table)+` TO STDOUT`)
	cmd := exec.CommandContext(ctx, "psql", args...)
	cmd.Env = c.env()
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	return nil
}

func (c *Cockroach) baseArgs() []string {
	return []string{
		"-h", c.cfg.DB.Host,
		"-p", strconv.Itoa(c.cfg.DB.Port),
		"-U", c.cfg.DB.User,
		"-d", c.cfg.DB.Name,
		"--no-psqlrc",
	}
}

func (c *Cockroach) env() []string {
	env := os.Environ()
	if c.cfg.DB.Password != "" {
		env = append(env, "PGPASSWORD="+c.cfg.DB.Password)
	}
	return env
}

// quoteIdent wraps an identifier in double quotes, escaping embedded quotes.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
