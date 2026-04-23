package verifier

import (
	"context"
	"strings"
	"testing"
)

const sqliteComplete = "BEGIN TRANSACTION;\nCREATE TABLE t(id);\nINSERT INTO t VALUES(1);\nCOMMIT;\n"
const sqliteTruncated = "BEGIN TRANSACTION;\nCREATE TABLE t(id);\nINSERT INTO t VALUES(1,(2"

func TestSQLite_Verify_WithCommit(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "s.sql.gz", sqliteComplete)
	if err := NewSQLite(discardLogger()).Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestSQLite_Verify_MissingCommit(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "s.sql.gz", sqliteTruncated)
	err := NewSQLite(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "does not end with COMMIT") {
		t.Errorf("err = %v", err)
	}
}

func TestSQLite_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "s.sql.gz", sqliteComplete)
	if err := NewSQLite(discardLogger()).Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}

func TestSQLite_RegisteredInFactory(t *testing.T) {
	found := false
	for _, dbt := range Registered() {
		if dbt == "sqlite" {
			found = true
			break
		}
	}
	if !found {
		t.Error("sqlite not registered in verifier registry")
	}
}
