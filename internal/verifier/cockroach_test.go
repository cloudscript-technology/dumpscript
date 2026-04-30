package verifier

import (
	"context"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestCockroach_Factory_ResolvesToPostgres(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBCockroach}, VerifyContent: true}
	v, err := New(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(*Postgres); !ok {
		t.Errorf("cockroach verifier should be *Postgres, got %T", v)
	}
}

func TestCockroach_Verify_UsesPostgresFooter(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "crdb.sql.gz", pgCompleteFooter)

	cfg := &config.Config{DB: config.DB{Type: config.DBCockroach}, VerifyContent: true}
	v, err := New(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestCockroach_RegisteredInFactory(t *testing.T) {
	found := false
	for _, dbt := range Registered() {
		if dbt == config.DBCockroach {
			found = true
			break
		}
	}
	if !found {
		t.Error("cockroach not registered in verifier registry")
	}
}
