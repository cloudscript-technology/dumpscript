package dumper

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// More engine tests, completing coverage that started with impl_test.go's
// postgres/mysql/mariadb/mongo. Same pattern: stub the CLI on PATH, run
// Dump, assert the artifact + decompressed payload match.

func TestRedis_Dump(t *testing.T) {
	// redis-cli --rdb writes to a real file path. The stub touches the file
	// at the path passed via --rdb, plus prints a fake RDB byte to stderr.
	installStub(t, "redis-cli", `
target=""
for arg in "$@"; do
  case "$prev" in --rdb) target="$arg" ;; esac
  prev="$arg"
done
printf "REDIS_RDB" > "$target"
`)
	cfg := baseCfg(t, config.DBRedis)
	cfg.DB.Port = 6379
	d := NewRedis(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if art.Extension != "rdb" {
		t.Errorf("extension = %s, want rdb", art.Extension)
	}
	if got := readGzip(t, art.Path); !strings.Contains(got, "REDIS_RDB") {
		t.Errorf("content = %q", got)
	}
}

func TestEtcd_Dump(t *testing.T) {
	installStub(t, "etcdctl", `
# etcdctl snapshot save <path>: stub writes a small marker to <path>.
target=""
for arg in "$@"; do prev2="$prev"; prev="$arg"; done
# argv ends with: ... snapshot save <path>
target="$prev"
printf "ETCD_SNAPSHOT" > "$target"
`)
	cfg := baseCfg(t, config.DBEtcd)
	cfg.DB.Port = 2379
	d := NewEtcd(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if art.Extension != "db" {
		t.Errorf("extension = %s, want db", art.Extension)
	}
	if got := readGzip(t, art.Path); !strings.Contains(got, "ETCD_SNAPSHOT") {
		t.Errorf("content = %q", got)
	}
}

func TestClickhouse_Dump(t *testing.T) {
	installStub(t, "clickhouse-client", `echo "native-bytes"`)
	cfg := baseCfg(t, config.DBClickhouse)
	cfg.DB.Port = 9000
	cfg.DB.Name = "appdb.users" // required: <database>.<table>
	d := NewClickhouse(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if art.Extension != "native" {
		t.Errorf("extension = %s, want native", art.Extension)
	}
	if got := readGzip(t, art.Path); got != "native-bytes\n" {
		t.Errorf("content = %q", got)
	}
}

func TestClickhouse_Dump_RejectsBadName(t *testing.T) {
	cfg := baseCfg(t, config.DBClickhouse)
	cfg.DB.Name = "no-dot-here"
	d := NewClickhouse(cfg, discardLogger())
	if _, err := d.Dump(context.Background()); err == nil {
		t.Fatal("expected error when DB_NAME lacks a dot")
	}
}

func TestSQLServer_Dump(t *testing.T) {
	installStub(t, "mssql-scripter", `echo "-- mssql script"`)
	cfg := baseCfg(t, config.DBSQLServer)
	cfg.DB.Port = 1433
	cfg.DB.Name = "appdb"
	d := NewSQLServer(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if art.Extension != "sql" {
		t.Errorf("extension = %s, want sql", art.Extension)
	}
	if got := readGzip(t, art.Path); got != "-- mssql script\n" {
		t.Errorf("content = %q", got)
	}
}

func TestSQLServer_Dump_RequiresDBName(t *testing.T) {
	cfg := baseCfg(t, config.DBSQLServer)
	cfg.DB.Name = ""
	d := NewSQLServer(cfg, discardLogger())
	if _, err := d.Dump(context.Background()); err == nil {
		t.Fatal("expected error when DB_NAME empty")
	}
}

func TestOracle_Dump(t *testing.T) {
	installStub(t, "exp", `echo "EXPORT:V19.00"`)
	cfg := baseCfg(t, config.DBOracle)
	cfg.DB.Port = 1521
	cfg.DB.Name = "ORCL"
	d := NewOracle(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if art.Extension != "dmp" {
		t.Errorf("extension = %s, want dmp", art.Extension)
	}
	if got := readGzip(t, art.Path); got != "EXPORT:V19.00\n" {
		t.Errorf("content = %q", got)
	}
}

func TestCockroach_Dump(t *testing.T) {
	// Cockroach's dumper runs psql multiple times: SHOW TABLES (list),
	// SHOW CREATE (per-table DDL with `|`-separated output), then COPY OUT
	// for data. The stub emits sane responses for each call by inspecting
	// argv for the query text.
	installStub(t, "psql", `
# Find the -c "<query>" argument.
query=""
prev=""
for arg in "$@"; do
  case "$prev" in -c) query="$arg" ;; esac
  prev="$arg"
done
case "$query" in
  *"SHOW TABLES"*|*"information_schema.tables"*)
    # Empty table list — no rows means no SHOW CREATE / COPY follow-up.
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`)
	cfg := baseCfg(t, config.DBCockroach)
	cfg.DB.Port = 26257
	cfg.DB.Name = "appdb"
	d := NewCockroach(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if art.Extension != "sql" {
		t.Errorf("extension = %s, want sql", art.Extension)
	}
}

func TestNeo4j_Dump(t *testing.T) {
	// neo4j-admin database dump --to-stdout writes a dump archive to
	// stdout — runDumpWithGzip wraps that in gzip and writes to disk.
	installStub(t, "neo4j-admin", `printf "NEO4J_DUMP_BYTES"`)
	cfg := baseCfg(t, config.DBNeo4j)
	cfg.DB.Port = 7687
	cfg.DB.Name = "neo4j"
	d := NewNeo4j(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if art.Extension != "neo4j" {
		t.Errorf("extension = %s, want neo4j", art.Extension)
	}
	if got := readGzip(t, art.Path); !strings.Contains(got, "NEO4J_DUMP_BYTES") {
		t.Errorf("content = %q", got)
	}
}

func TestSQLite_Dump(t *testing.T) {
	// sqlite is file-based — DB_NAME is the path. The stub reads that path
	// to prove the dumper passes it correctly, then emits a SQL-looking dump.
	installStub(t, "sqlite3", `
# argv: <path-to-sqlite> ... .dump
db="$1"
echo "-- dump of $db"
echo "COMMIT;"
`)
	cfg := baseCfg(t, config.DBSQLite)
	cfg.DB.Host = "" // sqlite doesn't need a host
	cfg.DB.Name = "/tmp/dummy.sqlite"
	d := NewSQLite(cfg, discardLogger())

	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	defer art.Cleanup()
	if art.Extension != "sql" {
		t.Errorf("extension = %s, want sql", art.Extension)
	}
	got := readGzip(t, art.Path)
	if !strings.Contains(got, "/tmp/dummy.sqlite") {
		t.Errorf("expected dumper to pass DB_NAME path, got: %q", got)
	}
	if !strings.Contains(got, "COMMIT;") {
		t.Errorf("expected SQL footer in output, got: %q", got)
	}
}

func TestSQLite_Dump_RequiresName(t *testing.T) {
	cfg := baseCfg(t, config.DBSQLite)
	cfg.DB.Name = ""
	d := NewSQLite(cfg, discardLogger())
	if _, err := d.Dump(context.Background()); err == nil {
		t.Fatal("expected error when DB_NAME empty for sqlite")
	}
}
