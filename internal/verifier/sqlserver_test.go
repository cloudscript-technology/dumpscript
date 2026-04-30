package verifier

import (
	"context"
	"strings"
	"testing"
)

const mssqlComplete = "CREATE TABLE [dbo].[t] (id INT)\nGO\nINSERT INTO [dbo].[t] VALUES (1)\nGO\n"
const mssqlTruncated = "CREATE TABLE [dbo].[t] (id INT)\nGO\nINSERT INTO [dbo].[t] VALUES (1,(2"

func TestSQLServer_Verify_WithFooter(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "m.sql.gz", mssqlComplete)
	if err := NewSQLServer(discardLogger()).Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestSQLServer_Verify_MissingFooter(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "m.sql.gz", mssqlTruncated)
	err := NewSQLServer(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "does not end with a GO batch terminator") {
		t.Errorf("err = %v", err)
	}
}

func TestSQLServer_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "m.sql.gz", mssqlComplete)
	if err := NewSQLServer(discardLogger()).Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}

func TestSQLServer_RegisteredInFactory(t *testing.T) {
	found := false
	for _, dbt := range Registered() {
		if dbt == "sqlserver" {
			found = true
			break
		}
	}
	if !found {
		t.Error("sqlserver not registered in verifier registry")
	}
}
