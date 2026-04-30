package dumper

import (
	"io"
	"log/slog"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNew_DispatchesByDBType(t *testing.T) {
	log := discardLogger()
	tests := []struct {
		dbType config.DBType
	}{
		{config.DBPostgres},
		{config.DBMySQL},
		{config.DBMariaDB},
		{config.DBMongo},
		{config.DBCockroach},
		{config.DBRedis},
		{config.DBSQLServer},
		{config.DBOracle},
		{config.DBElasticsearch},
		{config.DBEtcd},
		{config.DBClickhouse},
		{config.DBNeo4j},
		{config.DBSQLite},
	}
	for _, tc := range tests {
		t.Run(string(tc.dbType), func(t *testing.T) {
			cfg := &config.Config{DB: config.DB{Type: tc.dbType}}
			d, err := New(cfg, log)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			switch tc.dbType {
			case config.DBPostgres:
				if _, ok := d.(*Postgres); !ok {
					t.Errorf("got %T, want *Postgres", d)
				}
			case config.DBMySQL:
				if _, ok := d.(*MySQL); !ok {
					t.Errorf("got %T, want *MySQL", d)
				}
			case config.DBMariaDB:
				if _, ok := d.(*MariaDB); !ok {
					t.Errorf("got %T, want *MariaDB", d)
				}
			case config.DBMongo:
				if _, ok := d.(*Mongo); !ok {
					t.Errorf("got %T, want *Mongo", d)
				}
			case config.DBCockroach:
				if _, ok := d.(*Cockroach); !ok {
					t.Errorf("got %T, want *Cockroach", d)
				}
			case config.DBRedis:
				if _, ok := d.(*Redis); !ok {
					t.Errorf("got %T, want *Redis", d)
				}
			case config.DBSQLServer:
				if _, ok := d.(*SQLServer); !ok {
					t.Errorf("got %T, want *SQLServer", d)
				}
			case config.DBOracle:
				if _, ok := d.(*Oracle); !ok {
					t.Errorf("got %T, want *Oracle", d)
				}
			case config.DBElasticsearch:
				if _, ok := d.(*Elasticsearch); !ok {
					t.Errorf("got %T, want *Elasticsearch", d)
				}
			case config.DBEtcd:
				if _, ok := d.(*Etcd); !ok {
					t.Errorf("got %T, want *Etcd", d)
				}
			case config.DBClickhouse:
				if _, ok := d.(*Clickhouse); !ok {
					t.Errorf("got %T, want *Clickhouse", d)
				}
			case config.DBNeo4j:
				if _, ok := d.(*Neo4j); !ok {
					t.Errorf("got %T, want *Neo4j", d)
				}
			case config.DBSQLite:
				if _, ok := d.(*SQLite); !ok {
					t.Errorf("got %T, want *SQLite", d)
				}
			}
		})
	}
}

func TestNew_UnknownDBType(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBType("not-a-real-db")}}
	_, err := New(cfg, discardLogger())
	if err == nil {
		t.Fatal("expected error")
	}
}
