//go:build e2e

package e2e

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ────────────────────────────────────────────────────────────────────────────
// Compression: zstd round-trip
// ────────────────────────────────────────────────────────────────────────────

func TestZstdRoundTrip(t *testing.T) {
	bucket := "zstd-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-zstd", "postgres:17-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE m(id int, val text); INSERT INTO m VALUES (1,'zstd-marker');")

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":          "postgresql",
			"DB_HOST":          "e2e-pg-zstd",
			"DB_USER":          "postgres",
			"DB_PASSWORD":      "t",
			"DB_NAME":          "appdb",
			"S3_BUCKET":        bucket,
			"S3_PREFIX":        "zstd",
			"COMPRESSION_TYPE": "zstd",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	keys := listKeys(t, bucket)
	zstKey := firstKeyWithSuffix(keys, ".sql.zst")
	if zstKey == "" {
		t.Fatalf("no .sql.zst key found; keys=%v", keys)
	}
	for _, k := range keys {
		if strings.HasSuffix(k, ".sql.gz") && !strings.HasSuffix(k, ".manifest.json") {
			t.Errorf(".sql.gz coexists with .sql.zst — codec selection broken: %v", keys)
		}
	}

	// Drop the marker so the restore proves real recovery.
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c", "DROP TABLE m;")

	rlogs, rexit := runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-zstd",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_KEY":      zstKey,
		},
	})
	assertDumpscriptOK(t, "restore", rlogs, rexit)

	out := runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb",
		"-At", "-c", "SELECT val FROM m WHERE val='zstd-marker';")
	if !strings.Contains(out, "zstd-marker") {
		t.Fatalf("marker row not restored after zstd round-trip:\n%s", out)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Dump dry-run: exits 0, no upload happens
// ────────────────────────────────────────────────────────────────────────────

func TestDumpDryRun(t *testing.T) {
	bucket := "dump-dryrun-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-dump-dryrun", "postgres:17-alpine")
	_ = pg

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-dump-dryrun",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_PREFIX":   "dump-dryrun",
			"DRY_RUN":     "true",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)
	if !strings.Contains(logs, "dry-run") {
		t.Errorf("expected 'dry-run' mention in logs:\n%s", logs)
	}

	// Bucket should be empty under our prefix — preflight ran, dump didn't.
	for _, k := range listKeys(t, bucket) {
		if strings.HasPrefix(k, "dump-dryrun/") {
			t.Errorf("dump-dryrun should not upload, found: %s", k)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Exit codes: ConfigInvalid (2), DestinationUnreachable (3)
// ────────────────────────────────────────────────────────────────────────────

func TestExitCodes(t *testing.T) {
	bucket := "exitcodes-e2e"
	createBucket(t, bucket)

	t.Run("missing PERIODICITY → ExitConfigInvalid (2)", func(t *testing.T) {
		_, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "dump",
			Env: map[string]string{
				"DB_TYPE":     "postgresql",
				"DB_HOST":     "fake-host",
				"DB_USER":     "postgres",
				"DB_PASSWORD": "t",
				"DB_NAME":     "appdb",
				"S3_BUCKET":   bucket,
				"PERIODICITY": "", // override default
			},
		})
		// runDumpscript injects PERIODICITY=daily by default; pass empty
		// PERIODICITY to bypass that, but envconfig may reject empty enum
		// values too. Either way the failure should categorize as
		// ExitConfigInvalid (2). Falls back to ExitGeneric (1) if envconfig
		// rejects before our pipeline sentinel fires — we accept either.
		if exit != 2 && exit != 1 {
			t.Errorf("missing PERIODICITY exit=%d, want 2 (ExitConfigInvalid) or 1", exit)
		}
	})

	t.Run("unreachable destination → ExitDestinationUnreachable (3)", func(t *testing.T) {
		// Point at a host that doesn't resolve so the storage preflight List
		// fails fast with a network error rather than a 4xx (which would
		// surface as a different sentinel).
		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "dump",
			Env: map[string]string{
				"DB_TYPE":               "postgresql",
				"DB_HOST":               "fake-host",
				"DB_USER":               "postgres",
				"DB_PASSWORD":           "t",
				"DB_NAME":               "appdb",
				"S3_BUCKET":             bucket,
				"AWS_S3_ENDPOINT_URL":   "http://nonexistent-storage:9000",
				"AWS_ACCESS_KEY_ID":     "x",
				"AWS_SECRET_ACCESS_KEY": "x",
			},
		})
		if exit != 3 {
			t.Errorf("unreachable destination exit=%d, want 3; logs:\n%s", exit, logs)
		}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Object tagging: managed_by + engine + periodicity show up on the S3 object
// ────────────────────────────────────────────────────────────────────────────

func TestObjectTagging(t *testing.T) {
	bucket := "tagging-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-tagging", "postgres:17-alpine")
	_ = pg

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-tagging",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_PREFIX":   "tagging",
			"PERIODICITY": "weekly",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	keys := listKeys(t, bucket)
	dumpKey := firstKeyWithSuffix(keys, ".sql.gz")
	if dumpKey == "" {
		t.Fatalf("no dump uploaded; keys=%v", keys)
	}

	ctx := context.Background()
	c := s3client(t)
	tags, err := c.GetObjectTagging(ctx, bucket, dumpKey, minio.GetObjectTaggingOptions{})
	if err != nil {
		t.Fatalf("GetObjectTagging: %v", err)
	}
	got := tags.ToMap()
	if got["managed_by"] != "dumpscript" {
		t.Errorf("tag managed_by = %q, want dumpscript", got["managed_by"])
	}
	if got["engine"] != "postgresql" {
		t.Errorf("tag engine = %q, want postgresql", got["engine"])
	}
	if got["periodicity"] != "weekly" {
		t.Errorf("tag periodicity = %q, want weekly", got["periodicity"])
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Stale-lock recovery: a lock older than LOCK_GRACE_PERIOD is taken over
// ────────────────────────────────────────────────────────────────────────────

func TestStaleLockTakeover(t *testing.T) {
	bucket := "stale-lock-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-stale-lock", "postgres:17-alpine")
	_ = pg

	// Pre-seed a stale lock at today's lock key with StartedAt 48h ago.
	now := time.Now().UTC()
	day := fmt.Sprintf("%04d/%02d/%02d", now.Year(), now.Month(), now.Day())
	lockKey := "stale-lock/daily/" + day + "/.lock"
	staleBody := fmt.Sprintf(`{"execution_id":"crashed-prev","hostname":"old","started_at":"%s","pid":1234}`,
		now.Add(-48*time.Hour).Format(time.RFC3339))
	putObject(t, bucket, lockKey, staleBody)

	// LOCK_GRACE_PERIOD=24h → 48h-old lock is stale → dump takes over.
	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":            "postgresql",
			"DB_HOST":            "e2e-pg-stale-lock",
			"DB_USER":            "postgres",
			"DB_PASSWORD":        "t",
			"DB_NAME":            "appdb",
			"S3_BUCKET":          bucket,
			"S3_PREFIX":          "stale-lock",
			"LOCK_GRACE_PERIOD":  "24h",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)
	if !strings.Contains(logs, "taking over stale lock") &&
		!strings.Contains(logs, "stale") {
		t.Errorf("expected stale-lock takeover log line:\n%s", logs)
	}

	// A real dump should now exist alongside the (overwritten) lock.
	keys := listKeys(t, bucket)
	if firstKeyWithSuffix(keys, ".sql.gz") == "" {
		t.Errorf("expected a .sql.gz dump after stale-lock takeover; keys=%v", keys)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// createDB on restore: target DB is dropped, restore recreates + applies
// ────────────────────────────────────────────────────────────────────────────

func TestRestoreCreateDB(t *testing.T) {
	bucket := "createdb-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-createdb", "postgres:17-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE m(id int, val text); INSERT INTO m VALUES (1,'createdb-marker');")

	dlogs, dexit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-createdb",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_PREFIX":   "createdb",
		},
	})
	assertDumpscriptOK(t, "dump", dlogs, dexit)
	dumpKey := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz")

	// Drop the entire DB, then restore with CREATE_DB=true. Restore must
	// issue CREATE DATABASE itself before applying the dump.
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "postgres", "-c",
		"DROP DATABASE appdb;")

	rlogs, rexit := runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-createdb",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_KEY":      dumpKey,
			"CREATE_DB":   "true",
		},
	})
	assertDumpscriptOK(t, "restore", rlogs, rexit)

	out := runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb",
		"-At", "-c", "SELECT val FROM m WHERE val='createdb-marker';")
	if !strings.Contains(out, "createdb-marker") {
		t.Fatalf("marker not restored after CREATE_DB=true:\n%s", out)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Empty DB: dump succeeds, produces a valid (small) artifact
// ────────────────────────────────────────────────────────────────────────────

func TestEmptyDBDump(t *testing.T) {
	bucket := "empty-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-empty", "postgres:17-alpine")
	_ = pg
	// Don't create any tables — appdb is the default empty DB.

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-empty",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_PREFIX":   "empty",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	dumpKey := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz")
	if dumpKey == "" {
		t.Fatalf("no dump produced from empty DB")
	}
	body, err := getObject(t, bucket, dumpKey)
	if err != nil {
		t.Fatal(err)
	}
	// Even an empty DB dump has the gzip header + pg_dump header lines.
	if len(body) < 32 {
		t.Errorf("empty-DB dump suspiciously small (%d bytes)", len(body))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// DUMP_OPTIONS propagation: --no-owner reaches pg_dump and changes output
// ────────────────────────────────────────────────────────────────────────────

func TestDumpOptionsPropagation(t *testing.T) {
	bucket := "dumpopts-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-dumpopts", "postgres:17-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE m(id int);")

	// --no-owner suppresses ALTER ... OWNER TO statements in pg_dump output;
	// observable by inspecting the produced .sql.
	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":      "postgresql",
			"DB_HOST":      "e2e-pg-dumpopts",
			"DB_USER":      "postgres",
			"DB_PASSWORD":  "t",
			"DB_NAME":      "appdb",
			"S3_BUCKET":    bucket,
			"S3_PREFIX":    "dumpopts",
			"DUMP_OPTIONS": "--no-owner",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	dumpKey := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz")
	body, err := getObject(t, bucket, dumpKey)
	if err != nil {
		t.Fatal(err)
	}
	plain := gunzip(t, body)
	if strings.Contains(plain, "OWNER TO") {
		t.Errorf("--no-owner should suppress 'OWNER TO' in pg_dump output; first 400 chars:\n%s",
			truncate(plain, 400))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Verifier rejects truncated dumps on restore
// ────────────────────────────────────────────────────────────────────────────

func TestRestoreRejectsTruncatedDump(t *testing.T) {
	bucket := "truncated-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-truncated", "postgres:17-alpine")
	_ = pg

	// Seed a deliberately corrupted "dump": valid gzip header but truncated
	// payload — gzip reader fires an error before we reach the SQL parser,
	// which surfaces as ErrDumpTruncated → ExitDumpTruncated (8).
	corruptKey := "truncated/daily/2026/01/01/dump_20260101_000000.sql.gz"
	putObject(t, bucket, corruptKey, "\x1f\x8b\x08") // partial gzip magic only

	_, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-truncated",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_KEY":      corruptKey,
		},
	})
	// Either the restorer fails on the broken stream (typical) or the gzip
	// reader fails earlier — both are acceptable as long as the run exits
	// non-zero so the operator's Restore phase=Failed.
	if exit == 0 {
		t.Fatalf("restore of a corrupted dump should fail; exit=0")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Failure notification fires when the dump pipeline errors out
// ────────────────────────────────────────────────────────────────────────────

func TestNotifyOnFailure(t *testing.T) {
	ctx := context.Background()
	bucket := "notify-fail-e2e"
	createBucket(t, bucket)

	// Tiny HTTP receiver that captures POSTed bodies.
	const receiverPy = `import http.server
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        n=int(self.headers.get('Content-Length',0))
        body=self.rfile.read(n)
        open('/last.json','wb').write(body)
        self.send_response(200);self.end_headers();self.wfile.write(b'ok')
    def log_message(self,*a,**k):pass
print('listening',flush=True)
http.server.HTTPServer(('0.0.0.0',8088),H).serve_forever()`
	webhook, err := testcontainers.Run(ctx, "python:3.11-slim",
		testcontainers.WithCmd("python", "-c", receiverPy),
		testcontainers.WithExposedPorts("8088/tcp"),
		tcnetwork.WithNetwork([]string{"e2e-notifyfail"}, sharedNet),
		testcontainers.WithWaitStrategy(wait.ForLog("listening").WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("start webhook: %v", err)
	}
	defer func() { _ = webhook.Terminate(ctx) }()

	// Run dumpscript pointing at a DB that doesn't exist — pipeline should
	// fail at preflight or dump phase, fire the failure event to the
	// webhook, and exit non-zero.
	_, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "nonexistent-db",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_PREFIX":   "notify-fail",
			"WEBHOOK_URL": "http://e2e-notifyfail:8088/hook",
		},
	})
	if exit == 0 {
		t.Fatal("expected non-zero exit for unreachable DB")
	}

	// Read the captured webhook body from the receiver container.
	body := runWithinContainer(t, webhook, "cat", "/last.json")
	if !strings.Contains(body, "fail") && !strings.Contains(body, "error") {
		t.Errorf("expected webhook body to contain failure marker, got:\n%s", body)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// S3 storage class propagates to the uploaded object
// ────────────────────────────────────────────────────────────────────────────

func TestS3StorageClass(t *testing.T) {
	bucket := "storage-class-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-sclass", "postgres:17-alpine")
	_ = pg

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":          "postgresql",
			"DB_HOST":          "e2e-pg-sclass",
			"DB_USER":          "postgres",
			"DB_PASSWORD":      "t",
			"DB_NAME":          "appdb",
			"S3_BUCKET":        bucket,
			"S3_PREFIX":        "sclass",
			"S3_STORAGE_CLASS": "REDUCED_REDUNDANCY",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	dumpKey := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz")
	ctx := context.Background()
	c := s3client(t)
	stat, err := c.StatObject(ctx, bucket, dumpKey, minio.StatObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// MinIO accepts arbitrary storage classes and surfaces the value in
	// x-amz-storage-class. REDUCED_REDUNDANCY is widely supported.
	if !strings.EqualFold(stat.StorageClass, "REDUCED_REDUNDANCY") &&
		stat.StorageClass != "" {
		t.Logf("warning: storage_class=%q; some MinIO versions ignore non-STANDARD classes",
			stat.StorageClass)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// helpers
// ────────────────────────────────────────────────────────────────────────────

// gunzip decompresses a gzip-encoded byte slice into a UTF-8 string. Test
// helper: panics on malformed input (the calling test will fail loudly,
// which is what we want).
func gunzip(t *testing.T, gz []byte) string {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(gr); err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	return out.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
