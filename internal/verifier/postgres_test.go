package verifier

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

const pgCompleteFooter = `
--
-- PostgreSQL database, version 16.x
--

CREATE TABLE foo (id INT);
INSERT INTO foo VALUES (1);

--
-- PostgreSQL database dump complete
--
`

const pgClusterComplete = `
CREATE ROLE admin;

--
-- PostgreSQL database cluster dump complete
--
`

const pgTruncated = `
CREATE TABLE foo (id INT);
INSERT INTO foo VALUES (1),(2),(3),(4`

func TestPostgres_Verify_SingleDBFooter(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "pg.sql.gz", pgCompleteFooter)
	v := NewPostgres(discardLogger())
	if err := v.Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestPostgres_Verify_ClusterFooter(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "pg.sql.gz", pgClusterComplete)
	v := NewPostgres(discardLogger())
	if err := v.Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestPostgres_Verify_MissingFooter(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "pg.sql.gz", pgTruncated)
	v := NewPostgres(discardLogger())
	err := v.Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "footer missing") {
		t.Errorf("err = %v", err)
	}
}

func TestPostgres_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "pg.sql.gz", pgCompleteFooter)
	v := NewPostgres(discardLogger())
	if err := v.Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}
