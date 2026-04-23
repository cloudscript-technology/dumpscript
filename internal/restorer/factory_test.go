package restorer

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
	tests := []config.DBType{
		config.DBPostgres, config.DBMySQL, config.DBMariaDB, config.DBMongo,
		config.DBCockroach, config.DBRedis, config.DBSQLServer, config.DBOracle,
		config.DBElasticsearch, config.DBEtcd, config.DBClickhouse, config.DBNeo4j, config.DBSQLite,
	}
	for _, dbType := range tests {
		t.Run(string(dbType), func(t *testing.T) {
			cfg := &config.Config{DB: config.DB{Type: dbType}}
			r, err := New(cfg, log)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			switch dbType {
			case config.DBPostgres:
				if _, ok := r.(*Postgres); !ok {
					t.Errorf("got %T, want *Postgres", r)
				}
			case config.DBMySQL:
				if _, ok := r.(*MySQL); !ok {
					t.Errorf("got %T, want *MySQL", r)
				}
			case config.DBMariaDB:
				if _, ok := r.(*MariaDB); !ok {
					t.Errorf("got %T, want *MariaDB", r)
				}
			case config.DBMongo:
				if _, ok := r.(*Mongo); !ok {
					t.Errorf("got %T, want *Mongo", r)
				}
			case config.DBCockroach:
				if _, ok := r.(*Cockroach); !ok {
					t.Errorf("got %T, want *Cockroach", r)
				}
			case config.DBRedis:
				if _, ok := r.(*Redis); !ok {
					t.Errorf("got %T, want *Redis", r)
				}
			case config.DBSQLServer:
				if _, ok := r.(*SQLServer); !ok {
					t.Errorf("got %T, want *SQLServer", r)
				}
			case config.DBOracle:
				if _, ok := r.(*Oracle); !ok {
					t.Errorf("got %T, want *Oracle", r)
				}
			case config.DBElasticsearch:
				if _, ok := r.(*Elasticsearch); !ok {
					t.Errorf("got %T, want *Elasticsearch", r)
				}
			case config.DBEtcd:
				if _, ok := r.(*Etcd); !ok {
					t.Errorf("got %T, want *Etcd", r)
				}
			case config.DBClickhouse:
				if _, ok := r.(*Clickhouse); !ok {
					t.Errorf("got %T, want *Clickhouse", r)
				}
			case config.DBNeo4j:
				if _, ok := r.(*Neo4j); !ok {
					t.Errorf("got %T, want *Neo4j", r)
				}
			case config.DBSQLite:
				if _, ok := r.(*SQLite); !ok {
					t.Errorf("got %T, want *SQLite", r)
				}
			}
		})
	}
}

func TestNew_UnknownDBType(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBType("bogus")}}
	_, err := New(cfg, discardLogger())
	if err == nil {
		t.Fatal("expected error")
	}
}
