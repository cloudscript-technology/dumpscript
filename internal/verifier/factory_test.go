package verifier

import (
	"context"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestFactory_DispatchesByDBType(t *testing.T) {
	log := discardLogger()
	types := []config.DBType{config.DBPostgres, config.DBMySQL, config.DBMariaDB, config.DBMongo}
	for _, dbType := range types {
		t.Run(string(dbType), func(t *testing.T) {
			cfg := &config.Config{DB: config.DB{Type: dbType}, VerifyContent: true}
			v, err := New(cfg, log)
			if err != nil {
				t.Fatal(err)
			}
			switch dbType {
			case config.DBPostgres:
				if _, ok := v.(*Postgres); !ok {
					t.Errorf("got %T", v)
				}
			case config.DBMySQL, config.DBMariaDB:
				if _, ok := v.(*SQLFooter); !ok {
					t.Errorf("got %T", v)
				}
			case config.DBMongo:
				if _, ok := v.(*Mongo); !ok {
					t.Errorf("got %T", v)
				}
			}
		})
	}
}

func TestFactory_DisabledReturnsNoop(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBPostgres}, VerifyContent: false}
	v, err := New(cfg, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v.(Noop); !ok {
		t.Errorf("expected Noop when VerifyContent=false, got %T", v)
	}
}

func TestFactory_UnknownDBType(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBType("mssql")}, VerifyContent: true}
	_, err := New(cfg, discardLogger())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNoop_Accepts(t *testing.T) {
	if err := (Noop{}).Verify(context.Background(), "/any/path"); err != nil {
		t.Errorf("Noop should never error, got %v", err)
	}
}
