//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ---------------- CockroachDB ----------------
//
// CockroachDB speaks the Postgres wire protocol, so the image's pg_dump/psql
// clients drive both ends. Password auth is empty on the --insecure node.

func TestCockroach(t *testing.T) {
	ctx := context.Background()
	alias := "e2e-cockroach"

	ctr, err := testcontainers.Run(ctx, "cockroachdb/cockroach:v24.2.4",
		testcontainers.WithCmd("start-single-node", "--insecure"),
		testcontainers.WithExposedPorts("26257/tcp", "8080/tcp"),
		tcnetwork.WithNetwork([]string{alias}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{
				"./cockroach", "sql", "--insecure",
				"--host=localhost:26257", "-e", "SELECT 1;",
			}).WithStartupTimeout(90*time.Second).WithPollInterval(2*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start cockroach: %v", err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	runWithinContainer(t, ctr, "./cockroach", "sql", "--insecure",
		"--host=localhost:26257",
		"-e", `CREATE DATABASE appdb; USE appdb; CREATE TABLE t (id INT PRIMARY KEY, name STRING); INSERT INTO t VALUES (1,'alice'),(2,'bob'),(3,'carol');`)

	bucket := "cockroach-backups"
	createBucket(t, bucket)

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": "cockroach", "DB_HOST": alias, "DB_PORT": "26257",
			"DB_USER": "root", "DB_PASSWORD": "", "DB_NAME": "appdb",
			"S3_BUCKET": bucket, "S3_PREFIX": "crdb",
		},
	})
	assertDumpscriptOK(t, "cockroach dump", logs, code)

	key := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz")
	if key == "" {
		t.Fatalf("no cockroach dump key; keys=%v", listKeys(t, bucket))
	}

	runWithinContainer(t, ctr, "./cockroach", "sql", "--insecure",
		"--host=localhost:26257",
		"-e", "USE appdb; DROP TABLE t;")

	logs, code = runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE": "cockroach", "DB_HOST": alias, "DB_PORT": "26257",
			"DB_USER": "root", "DB_PASSWORD": "", "DB_NAME": "appdb",
			"S3_BUCKET": bucket, "S3_PREFIX": "crdb", "S3_KEY": key,
		},
	})
	assertDumpscriptOK(t, "cockroach restore", logs, code)

	got := runWithinContainer(t, ctr,
		"./cockroach", "sql", "--insecure", "--host=localhost:26257",
		"-e", "USE appdb; SELECT count(*) FROM t;", "--format=tsv")
	if !strings.Contains(got, "3") {
		t.Fatalf("cockroach: expected 3 rows; got %q", got)
	}
}

// ---------------- Redis ----------------
//
// RDB snapshot dump. Restore is intentionally unsupported by dumpscript
// (ErrRedisRestoreUnsupported), so this test validates the dump-only path.

func TestRedis(t *testing.T) {
	ctx := context.Background()
	alias := "e2e-redis"

	ctr, err := testcontainers.Run(ctx, "redis:7-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		tcnetwork.WithNetwork([]string{alias}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{"redis-cli", "PING"}).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	runWithinContainer(t, ctr, "redis-cli", "SET", "greeting", "hello-e2e")
	runWithinContainer(t, ctr, "redis-cli", "SET", "counter", "42")
	runWithinContainer(t, ctr, "redis-cli", "SAVE")

	bucket := "redis-backups"
	createBucket(t, bucket)

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": "redis", "DB_HOST": alias, "DB_PORT": "6379",
			"S3_BUCKET": bucket, "S3_PREFIX": "redis",
		},
	})
	assertDumpscriptOK(t, "redis dump", logs, code)

	key := firstKeyWithSuffix(listKeys(t, bucket), ".rdb.gz")
	if key == "" {
		t.Fatalf("no redis rdb key; keys=%v", listKeys(t, bucket))
	}
}

// ---------------- SQLite ----------------
//
// SQLite is file-based — a podman named volume is shared between a seeder
// container (which creates /data/app.sqlite), the dumpscript container (which
// dumps from and restores to that same file), and a verifier container (which
// counts rows after restore).

func TestSQLite(t *testing.T) {
	ctx := context.Background()
	volName := "e2e-sqlite-vol-" + time.Now().Format("150405")

	// Seed the DB file inside a fresh volume. Using a unique per-run name so
	// stale state from a previous interrupted run can't poison the seed step.
	seed, err := testcontainers.Run(ctx, dumpscriptImage,
		withSQLiteVolume(volName),
		testcontainers.WithEnv(map[string]string{}),
		testcontainers.WithConfigModifier(func(c *container.Config) {
			c.Entrypoint = []string{"sh"}
			c.Cmd = []string{"-c",
				`sqlite3 /data/app.sqlite "CREATE TABLE t(id INTEGER, name TEXT); INSERT INTO t VALUES (1,'alice'),(2,'bob'),(3,'carol');"`}
		}),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("seed sqlite: %v", err)
	}
	seedState, _ := seed.State(ctx)
	if seedState.ExitCode != 0 {
		rc, _ := seed.Logs(ctx)
		b, _ := io.ReadAll(rc)
		_ = seed.Terminate(ctx)
		t.Fatalf("seed sqlite exit=%d: %s", seedState.ExitCode, b)
	}
	_ = seed.Terminate(ctx)

	bucket := "sqlite-backups"
	createBucket(t, bucket)

	logs, code := runDumpscriptWithSQLiteVol(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": "sqlite", "DB_NAME": "/data/app.sqlite",
			"S3_BUCKET": bucket, "S3_PREFIX": "sqlite",
		},
	}, volName)
	assertDumpscriptOK(t, "sqlite dump", logs, code)

	key := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz")
	if key == "" {
		t.Fatalf("no sqlite dump key; keys=%v", listKeys(t, bucket))
	}

	// Wipe the DB file, then restore.
	wipe, err := testcontainers.Run(ctx, dumpscriptImage,
		withSQLiteVolume(volName),
		testcontainers.WithConfigModifier(func(c *container.Config) {
			c.Entrypoint = []string{"sh"}
			c.Cmd = []string{"-c", "rm -f /data/app.sqlite"}
		}),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("wipe sqlite: %v", err)
	}
	_ = wipe.Terminate(ctx)

	logs, code = runDumpscriptWithSQLiteVol(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE": "sqlite", "DB_NAME": "/data/app.sqlite",
			"S3_BUCKET": bucket, "S3_PREFIX": "sqlite", "S3_KEY": key,
		},
	}, volName)
	assertDumpscriptOK(t, "sqlite restore", logs, code)

	verify, err := testcontainers.Run(ctx, dumpscriptImage,
		withSQLiteVolume(volName),
		testcontainers.WithConfigModifier(func(c *container.Config) {
			c.Entrypoint = []string{"sh"}
			c.Cmd = []string{"-c", `sqlite3 /data/app.sqlite 'SELECT count(*) FROM t;'`}
		}),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("verify sqlite: %v", err)
	}
	defer func() { _ = verify.Terminate(ctx) }()
	rc, _ := verify.Logs(ctx)
	b, _ := io.ReadAll(rc)
	out := strings.TrimSpace(string(b))
	if !strings.Contains(out, "3") {
		t.Fatalf("sqlite restore: expected count=3, got %q", out)
	}
}

// withSQLiteVolume wires a named podman volume mount at /data via the
// low-level HostConfig modifier (testcontainers-go v0.42 has no higher-level
// helper for `testcontainers.Run`).
func withSQLiteVolume(volName string) testcontainers.CustomizeRequestOption {
	return testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
		hc.Mounts = append(hc.Mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: volName,
			Target: "/data",
		})
	})
}

// runDumpscriptWithSQLiteVol is a variant of runDumpscript that also attaches
// the shared SQLite volume. Duplicates the minimal env setup because the
// shared runDumpscript doesn't accept customizer options.
func runDumpscriptWithSQLiteVol(t *testing.T, r dumpscriptRun, volName string) (string, int) {
	t.Helper()
	ctx := context.Background()

	env := map[string]string{
		"STORAGE_BACKEND":       "s3",
		"AWS_REGION":            "us-east-1",
		"AWS_ACCESS_KEY_ID":     minioUser,
		"AWS_SECRET_ACCESS_KEY": minioPass,
		"AWS_S3_ENDPOINT_URL":   fmt.Sprintf("http://%s:9000", minioAlias),
		"PERIODICITY":           "daily",
		"RETENTION_DAYS":        "7",
		"LOG_LEVEL":             "info",
	}
	for k, v := range r.Env {
		env[k] = v
	}

	ctr, err := testcontainers.Run(ctx, dumpscriptImage,
		testcontainers.WithEnv(env),
		testcontainers.WithCmd(r.Subcmd),
		tcnetwork.WithNetwork(nil, sharedNet),
		withSQLiteVolume(volName),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(120*time.Second)),
	)
	if err != nil {
		t.Fatalf("start dumpscript %s: %v", r.Subcmd, err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	state, _ := ctr.State(ctx)
	rc, _ := ctr.Logs(ctx)
	buf, _ := io.ReadAll(rc)
	_ = rc.Close()
	return string(buf), state.ExitCode
}

// ---------------- etcd ----------------
//
// Snapshot dump via etcdctl. Restore is intentionally unsupported by
// dumpscript (ErrEtcdRestoreUnsupported) — test validates dump only.

func TestEtcd(t *testing.T) {
	ctx := context.Background()
	alias := "e2e-etcd"

	ctr, err := testcontainers.Run(ctx, "quay.io/coreos/etcd:v3.5.13",
		testcontainers.WithEnv(map[string]string{
			"ETCD_ADVERTISE_CLIENT_URLS": "http://0.0.0.0:2379",
			"ETCD_LISTEN_CLIENT_URLS":    "http://0.0.0.0:2379",
		}),
		testcontainers.WithExposedPorts("2379/tcp"),
		tcnetwork.WithNetwork([]string{alias}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForExec([]string{
				"etcdctl", "--endpoints=http://localhost:2379",
				"endpoint", "health",
			}).WithStartupTimeout(60*time.Second).WithPollInterval(2*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start etcd: %v", err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	runWithinContainer(t, ctr,
		"etcdctl", "--endpoints=http://localhost:2379", "put", "greeting", "hello-e2e")
	runWithinContainer(t, ctr,
		"etcdctl", "--endpoints=http://localhost:2379", "put", "counter", "42")

	bucket := "etcd-backups"
	createBucket(t, bucket)

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": "etcd", "DB_HOST": alias, "DB_PORT": "2379",
			"S3_BUCKET": bucket, "S3_PREFIX": "etcd",
		},
	})
	assertDumpscriptOK(t, "etcd dump", logs, code)

	key := firstKeyWithSuffix(listKeys(t, bucket), ".db.gz")
	if key == "" {
		t.Fatalf("no etcd dump key; keys=%v", listKeys(t, bucket))
	}
}

// ---------------- Elasticsearch ----------------
//
// Pure-Go scroll dumper + _bulk restorer (no external ES client in the image).

func TestElasticsearch(t *testing.T) {
	ctx := context.Background()
	alias := "e2e-elasticsearch"

	ctr, err := testcontainers.Run(ctx, "docker.elastic.co/elasticsearch/elasticsearch:8.13.0",
		testcontainers.WithEnv(map[string]string{
			"discovery.type":         "single-node",
			"xpack.security.enabled": "false",
			"ES_JAVA_OPTS":           "-Xms512m -Xmx512m",
		}),
		testcontainers.WithExposedPorts("9200/tcp"),
		tcnetwork.WithNetwork([]string{alias}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/_cluster/health").WithPort("9200/tcp").
				WithStatusCodeMatcher(func(status int) bool { return status == http.StatusOK }).
				WithStartupTimeout(180*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start elasticsearch: %v", err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	// Seed 3 docs + force refresh so they are visible to search/scroll.
	runWithinContainer(t, ctr, "sh", "-c",
		`curl -s -X POST 'http://localhost:9200/_bulk?refresh=true' -H 'Content-Type: application/x-ndjson' --data-binary '
{"index":{"_index":"myidx","_id":"a"}}
{"k":1,"tag":"alpha"}
{"index":{"_index":"myidx","_id":"b"}}
{"k":2,"tag":"beta"}
{"index":{"_index":"myidx","_id":"c"}}
{"k":3,"tag":"gamma"}
'`)

	bucket := "es-backups"
	createBucket(t, bucket)

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": "elasticsearch", "DB_HOST": alias, "DB_PORT": "9200",
			"DB_NAME":   "myidx",
			"S3_BUCKET": bucket, "S3_PREFIX": "es",
		},
	})
	assertDumpscriptOK(t, "es dump", logs, code)

	key := firstKeyWithSuffix(listKeys(t, bucket), ".ndjson.gz")
	if key == "" {
		t.Fatalf("no es dump key; keys=%v", listKeys(t, bucket))
	}

	runWithinContainer(t, ctr, "sh", "-c",
		`curl -s -X DELETE 'http://localhost:9200/myidx' >/dev/null`)

	logs, code = runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE": "elasticsearch", "DB_HOST": alias, "DB_PORT": "9200",
			"DB_NAME":   "myidx",
			"S3_BUCKET": bucket, "S3_PREFIX": "es", "S3_KEY": key,
		},
	})
	assertDumpscriptOK(t, "es restore", logs, code)

	runWithinContainer(t, ctr, "sh", "-c",
		`curl -s -X POST 'http://localhost:9200/myidx/_refresh' >/dev/null`)
	countOut := runWithinContainer(t, ctr, "sh", "-c",
		`curl -s 'http://localhost:9200/myidx/_count'`)
	if !strings.Contains(countOut, `"count":3`) {
		t.Fatalf("es restore: expected count=3; got %q", countOut)
	}
}
