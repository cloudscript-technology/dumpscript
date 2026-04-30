package verifier

import (
	"context"
	"strings"
	"testing"
)

const mysqlComplete = `CREATE TABLE users (id INT);
INSERT INTO users VALUES (1);
-- Dump completed on 2025-03-24 12:00:00
`

const mysqlTruncated = `CREATE TABLE users (id INT);
INSERT INTO users VALUES (1),(2),(3),(`

func TestSQLFooter_MySQL_Complete(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "m.sql.gz", mysqlComplete)
	v := NewMySQL(discardLogger())
	if err := v.Verify(context.Background(), p); err != nil {
		t.Errorf("unexpected: %v", err)
	}
}

func TestSQLFooter_MariaDB_Complete(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "m.sql.gz", mysqlComplete)
	v := NewMariaDB(discardLogger())
	if err := v.Verify(context.Background(), p); err != nil {
		t.Errorf("unexpected: %v", err)
	}
}

func TestSQLFooter_MissingFooter(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "m.sql.gz", mysqlTruncated)
	tests := []struct {
		name string
		v    Verifier
	}{
		{"mysql", NewMySQL(discardLogger())},
		{"mariadb", NewMariaDB(discardLogger())},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.v.Verify(context.Background(), p)
			if err == nil || !strings.Contains(err.Error(), "-- Dump completed") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestSQLFooter_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "m.sql.gz", mysqlComplete)
	v := NewMySQL(discardLogger())
	if err := v.Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}

func TestSQLFooter_EngineNameInError(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "m.sql.gz", mysqlTruncated)

	errM := NewMySQL(discardLogger()).Verify(context.Background(), p)
	if errM == nil || !strings.Contains(errM.Error(), "mysql") {
		t.Errorf("mysql err should mention engine: %v", errM)
	}
	errMa := NewMariaDB(discardLogger()).Verify(context.Background(), p)
	if errMa == nil || !strings.Contains(errMa.Error(), "mariadb") {
		t.Errorf("mariadb err should mention engine: %v", errMa)
	}
}
