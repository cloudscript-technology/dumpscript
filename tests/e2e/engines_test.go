//go:build e2e

package e2e

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ---------------- Postgres ----------------

// postgresVersions lists every server release the client (pg_dump 18) supports
// dumping. The e2e harness exercises a roundtrip against each one to guarantee
// backward compatibility of the single-image approach.
var postgresVersions = []string{"13", "14", "15", "16", "17", "18"}

func startPostgres(t *testing.T, alias, image string) testcontainers.Container {
	t.Helper()
	ctx := context.Background()
	ctr, err := testcontainers.Run(ctx, image,
		testcontainers.WithEnv(map[string]string{
			"POSTGRES_PASSWORD": "t",
			"POSTGRES_DB":       "appdb",
		}),
		testcontainers.WithExposedPorts("5432/tcp"),
		tcnetwork.WithNetwork([]string{alias}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres (%s): %v", image, err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	return ctr
}

func postgresRoundtrip(t *testing.T, version string) {
	t.Helper()
	alias := "e2e-pg-" + version
	image := "postgres:" + version + "-alpine"
	pg := startPostgres(t, alias, image)

	runWithinContainer(t, pg,
		"psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE t(id int, name text); INSERT INTO t VALUES (1,'alice'),(2,'bob');")

	bucket := "pg" + version + "-backups"
	createBucket(t, bucket)

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": "postgresql", "DB_HOST": alias, "DB_PORT": "5432",
			"DB_USER": "postgres", "DB_PASSWORD": "t", "DB_NAME": "appdb",
			"S3_BUCKET": bucket, "S3_PREFIX": "pg" + version,
		},
	})
	assertDumpscriptOK(t, "dump pg"+version, logs, code)

	key := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz")
	if key == "" {
		t.Fatalf("no dump key for pg%s; keys=%v", version, listKeys(t, bucket))
	}

	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c", "DROP TABLE t;")

	logs, code = runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE": "postgresql", "DB_HOST": alias, "DB_PORT": "5432",
			"DB_USER": "postgres", "DB_PASSWORD": "t", "DB_NAME": "appdb",
			"S3_BUCKET": bucket, "S3_PREFIX": "pg" + version, "S3_KEY": key,
		},
	})
	assertDumpscriptOK(t, "restore pg"+version, logs, code)

	got := strings.TrimSpace(runWithinContainer(t, pg,
		"psql", "-U", "postgres", "-d", "appdb", "-tA", "-c", "SELECT count(*) FROM t;"))
	if got != "2" {
		t.Fatalf("pg%s: expected 2 rows, got %q", version, got)
	}
}

// TestPostgres exercises a dump+restore roundtrip against every supported
// Postgres server version (13–18) using the single pg_dump 18 client shipped
// in the dumpscript image.
func TestPostgres(t *testing.T) {
	for _, v := range postgresVersions {
		v := v
		t.Run("pg"+v, func(t *testing.T) { postgresRoundtrip(t, v) })
	}
}

func TestPostgresCluster(t *testing.T) {
	alias := "e2e-pg-cluster"
	pg := startPostgres(t, alias, "postgres:16-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "postgres", "-c", "CREATE DATABASE db1;")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "postgres", "-c", "CREATE DATABASE db2;")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "db1", "-c",
		"CREATE TABLE t(x int); INSERT INTO t VALUES (1),(2);")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "db2", "-c",
		"CREATE TABLE t(x int); INSERT INTO t VALUES (10),(20),(30);")

	bucket := "pgall-backups"
	createBucket(t, bucket)

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": "postgresql", "DB_HOST": alias, "DB_PORT": "5432",
			"DB_USER": "postgres", "DB_PASSWORD": "t",
			"S3_BUCKET": bucket, "S3_PREFIX": "pg-all",
		},
	})
	assertDumpscriptOK(t, "pg_dumpall", logs, code)

	key := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz")
	if key == "" {
		t.Fatalf("no pg_dumpall key in bucket")
	}

	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "postgres", "-c", "DROP DATABASE db1;")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "postgres", "-c", "DROP DATABASE db2;")

	logs, code = runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE": "postgresql", "DB_HOST": alias, "DB_PORT": "5432",
			"DB_USER": "postgres", "DB_PASSWORD": "t",
			"S3_BUCKET": bucket, "S3_PREFIX": "pg-all", "S3_KEY": key,
		},
	})
	assertDumpscriptOK(t, "restore pg_dumpall", logs, code)

	c1 := strings.TrimSpace(runWithinContainer(t, pg,
		"psql", "-U", "postgres", "-d", "db1", "-tA", "-c", "SELECT count(*) FROM t;"))
	c2 := strings.TrimSpace(runWithinContainer(t, pg,
		"psql", "-U", "postgres", "-d", "db2", "-tA", "-c", "SELECT count(*) FROM t;"))
	if c1 != "2" || c2 != "3" {
		t.Fatalf("expected db1=2 db2=3; got db1=%q db2=%q", c1, c2)
	}
}

// ---------------- MariaDB / MySQL family ----------------

type mysqlFamily struct {
	Label       string
	Alias       string
	Image       string
	PasswordEnv string
	DatabaseEnv string
	DBType      string
	Platform    string
	ClientCmd   string // mysql or mariadb (MariaDB 11.x images only ship `mariadb`)
}

func startMySQLFamily(t *testing.T, cfg mysqlFamily) testcontainers.Container {
	t.Helper()
	ctx := context.Background()
	client := cfg.ClientCmd
	if client == "" {
		client = "mysql"
	}
	opts := []testcontainers.ContainerCustomizer{
		testcontainers.WithEnv(map[string]string{
			cfg.PasswordEnv: "t",
			cfg.DatabaseEnv: "appdb",
		}),
		testcontainers.WithExposedPorts("3306/tcp"),
		tcnetwork.WithNetwork([]string{cfg.Alias}, sharedNet),
		// "ready for connections" fires before root user setup completes in
		// MySQL 8.0. Use a real authenticated query instead.
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{
				client, "-uroot", "-pt", "-e", "USE appdb; SELECT 1;",
			}).WithStartupTimeout(120*time.Second).WithPollInterval(2*time.Second),
		),
	}
	if cfg.Platform != "" {
		opts = append(opts, testcontainers.WithImagePlatform(cfg.Platform))
	}
	ctr, err := testcontainers.Run(ctx, cfg.Image, opts...)
	if err != nil {
		t.Fatalf("start %s: %v", cfg.Label, err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	return ctr
}

func runMySQLFamilyRoundtrip(t *testing.T, cfg mysqlFamily, seedRows int) {
	t.Helper()
	ctr := startMySQLFamily(t, cfg)

	vals := "(1,'alice'),(2,'bob')"
	if seedRows == 3 {
		vals = "(1,'alice'),(2,'bob'),(3,'carol')"
	}
	client := cfg.ClientCmd
	if client == "" {
		client = "mysql"
	}
	runWithinContainer(t, ctr, client, "-uroot", "-pt", "appdb", "-e",
		"CREATE TABLE t(id int, name varchar(32)); INSERT INTO t VALUES "+vals+";")

	bucket := cfg.Label + "-backups"
	createBucket(t, bucket)

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": cfg.DBType, "DB_HOST": cfg.Alias, "DB_PORT": "3306",
			"DB_USER": "root", "DB_PASSWORD": "t", "DB_NAME": "appdb",
			"S3_BUCKET": bucket, "S3_PREFIX": cfg.Label,
		},
	})
	assertDumpscriptOK(t, "dump", logs, code)

	key := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz")
	if key == "" {
		t.Fatalf("no dump key in bucket")
	}

	runWithinContainer(t, ctr, client, "-uroot", "-pt", "appdb", "-e", "DROP TABLE t;")

	logs, code = runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE": cfg.DBType, "DB_HOST": cfg.Alias, "DB_PORT": "3306",
			"DB_USER": "root", "DB_PASSWORD": "t", "DB_NAME": "appdb",
			"S3_BUCKET": bucket, "S3_PREFIX": cfg.Label, "S3_KEY": key,
		},
	})
	assertDumpscriptOK(t, "restore", logs, code)

	rawOut := runWithinContainer(t, ctr,
		client, "-uroot", "-pt", "appdb", "-BN", "-e", "SELECT count(*) FROM t;")
	// MySQL 8.0 prints "Using a password ... insecure" warning on stderr
	// which lands in the stream. Pick the first all-digit token.
	got := ""
	for _, line := range strings.Split(rawOut, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.Contains(strings.ToLower(line), "warning") {
			got = line
			break
		}
	}
	if got != strconv.Itoa(seedRows) {
		t.Fatalf("expected %d rows, got %q (raw=%q)", seedRows, got, rawOut)
	}
}

func TestMariaDB(t *testing.T) {
	runMySQLFamilyRoundtrip(t, mysqlFamily{
		Label:       "mariadb",
		Alias:       "e2e-mariadb",
		Image:       "mariadb:11.4",
		PasswordEnv: "MARIADB_ROOT_PASSWORD",
		DatabaseEnv: "MARIADB_DATABASE",
		DBType:      "mariadb",
		ClientCmd:   "mariadb", // mariadb:11.4 dropped mysql symlink
	}, 3)
}

func TestMySQL57(t *testing.T) {
	runMySQLFamilyRoundtrip(t, mysqlFamily{
		Label:       "mysql57",
		Alias:       "e2e-mysql57",
		Image:       "mysql:5.7",
		PasswordEnv: "MYSQL_ROOT_PASSWORD",
		DatabaseEnv: "MYSQL_DATABASE",
		DBType:      "mysql",
		Platform:    "linux/amd64",
	}, 2)
}

func TestMySQL80(t *testing.T) {
	runMySQLFamilyRoundtrip(t, mysqlFamily{
		Label:       "mysql80",
		Alias:       "e2e-mysql80",
		Image:       "mysql:8.0",
		PasswordEnv: "MYSQL_ROOT_PASSWORD",
		DatabaseEnv: "MYSQL_DATABASE",
		DBType:      "mysql",
	}, 2)
}

// ---------------- Mongo ----------------

func TestMongo(t *testing.T) {
	ctx := context.Background()
	alias := "e2e-mongo"
	ctr, err := testcontainers.Run(ctx, "mongo:7",
		testcontainers.WithEnv(map[string]string{
			"MONGO_INITDB_ROOT_USERNAME": "admin",
			"MONGO_INITDB_ROOT_PASSWORD": "t",
		}),
		testcontainers.WithExposedPorts("27017/tcp"),
		tcnetwork.WithNetwork([]string{alias}, sharedNet),
		// "Waiting for connections" logs BEFORE the root user is created.
		// Use an authenticated ping via mongosh instead.
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{
				"mongosh", "-u", "admin", "-p", "t",
				"--authenticationDatabase", "admin",
				"--quiet", "--eval", "db.runCommand({ping:1}).ok",
			}).WithStartupTimeout(60*time.Second).WithPollInterval(1*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start mongo: %v", err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	runWithinContainer(t, ctr, "mongosh",
		"-u", "admin", "-p", "t", "--authenticationDatabase", "admin", "--quiet",
		"--eval", "use('appdb'); db.t.insertMany([{x:1},{x:2},{x:3},{x:4}])")

	bucket := "mongo-backups"
	createBucket(t, bucket)

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": "mongodb", "DB_HOST": alias, "DB_PORT": "27017",
			"DB_USER": "admin", "DB_PASSWORD": "t", "DB_NAME": "appdb",
			"DUMP_OPTIONS": "--authenticationDatabase=admin",
			"S3_BUCKET":    bucket, "S3_PREFIX": "mongo",
		},
	})
	assertDumpscriptOK(t, "mongo dump", logs, code)

	key := firstKeyWithSuffix(listKeys(t, bucket), ".archive.gz")
	if key == "" {
		t.Fatalf("no mongo archive in bucket")
	}

	runWithinContainer(t, ctr, "mongosh",
		"-u", "admin", "-p", "t", "--authenticationDatabase", "admin", "--quiet",
		"--eval", "use('appdb'); db.t.drop()")

	logs, code = runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE": "mongodb", "DB_HOST": alias, "DB_PORT": "27017",
			"DB_USER": "admin", "DB_PASSWORD": "t", "DB_NAME": "appdb",
			"DUMP_OPTIONS": "--authenticationDatabase=admin",
			"S3_BUCKET":    bucket, "S3_PREFIX": "mongo", "S3_KEY": key,
		},
	})
	assertDumpscriptOK(t, "mongo restore", logs, code)

	got := runWithinContainer(t, ctr, "mongosh",
		"-u", "admin", "-p", "t", "--authenticationDatabase", "admin", "--quiet",
		"--eval", "use('appdb'); print(db.t.countDocuments({}))")
	fields := strings.Fields(got)
	if len(fields) == 0 || fields[len(fields)-1] != "4" {
		t.Fatalf("expected 4 docs, got %q", got)
	}
}
