//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ────────────────────────────────────────────────────────────────────────────
// Periodicity: dump produces the right S3 key prefix layout per period
// ────────────────────────────────────────────────────────────────────────────

func TestPeriodicityLayouts(t *testing.T) {
	bucket := "periodicity-e2e"
	createBucket(t, bucket)
	pg := startPostgres(t, "e2e-pg-periodicity", "postgres:17-alpine")
	_ = pg

	cases := []struct {
		periodicity string
		// Path segment after `<prefix>/<periodicity>/`. Date-based for daily;
		// daily layout: YYYY/MM/DD; weekly: YYYY/WW; monthly: YYYY/MM;
		// yearly: YYYY.
		expectSubstrAfterPeriodicity []string
	}{
		{"daily", []string{"daily/"}},
		{"weekly", []string{"weekly/"}},
		{"monthly", []string{"monthly/"}},
		{"yearly", []string{"yearly/"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.periodicity, func(t *testing.T) {
			prefix := "p-" + tc.periodicity
			logs, exit := runDumpscript(t, dumpscriptRun{
				Subcmd: "dump",
				Env: map[string]string{
					"DB_TYPE":     "postgresql",
					"DB_HOST":     "e2e-pg-periodicity",
					"DB_USER":     "postgres",
					"DB_PASSWORD": "t",
					"DB_NAME":     "appdb",
					"S3_BUCKET":   bucket,
					"S3_PREFIX":   prefix,
					"PERIODICITY": tc.periodicity,
				},
			})
			assertDumpscriptOK(t, "dump", logs, exit)
			keys := listKeys(t, bucket)
			var found bool
			for _, k := range keys {
				if strings.HasPrefix(k, prefix+"/"+tc.periodicity+"/") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no key under %s/%s/ found; keys=%v",
					prefix, tc.periodicity, keys)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Cleanup subcommand: deletes dumps older than RETENTION_DAYS
// ────────────────────────────────────────────────────────────────────────────

func TestCleanupSubcommand(t *testing.T) {
	bucket := "cleanup-e2e"
	createBucket(t, bucket)

	// Pre-seed three dump-shaped objects: one fresh, one old (well beyond
	// retention), one ancient. Cleanup should remove the older two and
	// keep the fresh one.
	now := time.Now().UTC()
	freshPath := fmt.Sprintf("p/daily/%04d/%02d/%02d/dump_x.sql.gz",
		now.Year(), int(now.Month()), now.Day())
	oldDate := now.AddDate(0, 0, -30)
	oldPath := fmt.Sprintf("p/daily/%04d/%02d/%02d/dump_x.sql.gz",
		oldDate.Year(), int(oldDate.Month()), oldDate.Day())
	ancientDate := now.AddDate(-1, 0, 0)
	ancientPath := fmt.Sprintf("p/daily/%04d/%02d/%02d/dump_x.sql.gz",
		ancientDate.Year(), int(ancientDate.Month()), ancientDate.Day())
	for _, p := range []string{freshPath, oldPath, ancientPath} {
		putObject(t, bucket, p, "stub")
	}

	t.Run("dry-run logs would-delete but keeps everything", func(t *testing.T) {
		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "cleanup",
			Env: map[string]string{
				"DB_TYPE":        "postgresql",
				"S3_BUCKET":      bucket,
				"S3_PREFIX":      "p",
				"RETENTION_DAYS": "7",
				"DRY_RUN":        "true",
			},
		})
		if exit != 0 {
			t.Fatalf("cleanup --dry-run exit=%d; logs:\n%s", exit, logs)
		}
		if !strings.Contains(logs, "dry-run") {
			t.Errorf("expected 'dry-run' in logs:\n%s", logs)
		}
		// All three still present.
		keys := listKeys(t, bucket)
		for _, want := range []string{freshPath, oldPath, ancientPath} {
			if !containsKey(keys, want) {
				t.Errorf("dry-run should keep %q; keys=%v", want, keys)
			}
		}
	})

	t.Run("real cleanup deletes old, keeps fresh", func(t *testing.T) {
		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "cleanup",
			Env: map[string]string{
				"DB_TYPE":        "postgresql",
				"S3_BUCKET":      bucket,
				"S3_PREFIX":      "p",
				"RETENTION_DAYS": "7",
			},
		})
		if exit != 0 {
			t.Fatalf("cleanup exit=%d; logs:\n%s", exit, logs)
		}
		keys := listKeys(t, bucket)
		if !containsKey(keys, freshPath) {
			t.Errorf("fresh dump %q should be kept; keys=%v", freshPath, keys)
		}
		if containsKey(keys, oldPath) {
			t.Errorf("30-day-old dump %q should be deleted; keys=%v", oldPath, keys)
		}
		if containsKey(keys, ancientPath) {
			t.Errorf("1-year-old dump %q should be deleted; keys=%v", ancientPath, keys)
		}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// DUMP_TIMEOUT cancels mid-dump
// ────────────────────────────────────────────────────────────────────────────

func TestDumpTimeoutCancellation(t *testing.T) {
	bucket := "timeout-e2e"
	createBucket(t, bucket)
	pg := startPostgres(t, "e2e-pg-timeout", "postgres:17-alpine")
	_ = pg

	// 1ms timeout — even a small DB can't dump that fast. Pipeline should
	// abort with context cancel, exit non-zero. We're testing that the
	// timeout *fires* (not that it surfaces as a specific exit code,
	// since context-cancel can manifest as several sentinels).
	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":      "postgresql",
			"DB_HOST":      "e2e-pg-timeout",
			"DB_USER":      "postgres",
			"DB_PASSWORD":  "t",
			"DB_NAME":      "appdb",
			"S3_BUCKET":    bucket,
			"S3_PREFIX":    "timeout",
			"DUMP_TIMEOUT": "1ms",
		},
	})
	if exit == 0 {
		t.Fatalf("DUMP_TIMEOUT=1ms should abort the run; exit=0\nlogs:\n%s", logs)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// ENCRYPTION_KEY validation: too short, malformed → fails at startup
// ────────────────────────────────────────────────────────────────────────────

func TestEncryptionKeyValidation(t *testing.T) {
	bucket := "enc-validation-e2e"
	createBucket(t, bucket)
	pg := startPostgres(t, "e2e-pg-enc-validation", "postgres:17-alpine")
	_ = pg

	t.Run("too-short hex key → non-zero exit", func(t *testing.T) {
		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "dump",
			Env: map[string]string{
				"DB_TYPE":        "postgresql",
				"DB_HOST":        "e2e-pg-enc-validation",
				"DB_USER":        "postgres",
				"DB_PASSWORD":    "t",
				"DB_NAME":        "appdb",
				"S3_BUCKET":      bucket,
				"S3_PREFIX":      "enc-short",
				"ENCRYPTION_KEY": "deadbeef", // 4 bytes after hex decode
			},
		})
		if exit == 0 {
			t.Fatalf("short ENCRYPTION_KEY should fail; logs:\n%s", logs)
		}
	})

	t.Run("non-hex key → non-zero exit", func(t *testing.T) {
		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "dump",
			Env: map[string]string{
				"DB_TYPE":        "postgresql",
				"DB_HOST":        "e2e-pg-enc-validation",
				"DB_USER":        "postgres",
				"DB_PASSWORD":    "t",
				"DB_NAME":        "appdb",
				"S3_BUCKET":      bucket,
				"S3_PREFIX":      "enc-bad",
				"ENCRYPTION_KEY": "this-is-not-hex-at-all-just-text",
			},
		})
		if exit == 0 {
			t.Fatalf("non-hex ENCRYPTION_KEY should fail; logs:\n%s", logs)
		}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Restore failure modes: missing key, wrong encryption key
// ────────────────────────────────────────────────────────────────────────────

func TestRestoreFailureModes(t *testing.T) {
	bucket := "restore-fail-e2e"
	createBucket(t, bucket)
	pg := startPostgres(t, "e2e-pg-restore-fail", "postgres:17-alpine")
	_ = pg

	t.Run("missing source key → non-zero exit", func(t *testing.T) {
		_, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "restore",
			Env: map[string]string{
				"DB_TYPE":     "postgresql",
				"DB_HOST":     "e2e-pg-restore-fail",
				"DB_USER":     "postgres",
				"DB_PASSWORD": "t",
				"DB_NAME":     "appdb",
				"S3_BUCKET":   bucket,
				"S3_KEY":      "does/not/exist.sql.gz",
			},
		})
		if exit == 0 {
			t.Fatal("restore with missing source key should fail")
		}
	})

	t.Run("encrypted dump + wrong key → non-zero exit", func(t *testing.T) {
		// Produce an encrypted dump with key A.
		const keyA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
		const keyB = "1111111111111111111111111111111111111111111111111111111111111111"

		runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
			"CREATE TABLE m(id int);")

		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "dump",
			Env: map[string]string{
				"DB_TYPE":        "postgresql",
				"DB_HOST":        "e2e-pg-restore-fail",
				"DB_USER":        "postgres",
				"DB_PASSWORD":    "t",
				"DB_NAME":        "appdb",
				"S3_BUCKET":      bucket,
				"S3_PREFIX":      "wrong-key",
				"ENCRYPTION_KEY": keyA,
			},
		})
		assertDumpscriptOK(t, "dump", logs, exit)

		aesKey := firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz.aes")
		if aesKey == "" {
			t.Fatalf("no .aes key produced; keys=%v", listKeys(t, bucket))
		}

		_, rexit := runDumpscript(t, dumpscriptRun{
			Subcmd: "restore",
			Env: map[string]string{
				"DB_TYPE":        "postgresql",
				"DB_HOST":        "e2e-pg-restore-fail",
				"DB_USER":        "postgres",
				"DB_PASSWORD":    "t",
				"DB_NAME":        "appdb",
				"S3_BUCKET":      bucket,
				"S3_KEY":         aesKey,
				"ENCRYPTION_KEY": keyB,
			},
		})
		if rexit == 0 {
			t.Fatal("restore with wrong ENCRYPTION_KEY should fail")
		}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// Multiple notifiers: Slack + Webhook + Stdout all fire on success
// ────────────────────────────────────────────────────────────────────────────

func TestMultiNotifier(t *testing.T) {
	ctx := context.Background()
	bucket := "multi-notify-e2e"
	createBucket(t, bucket)
	pg := startPostgres(t, "e2e-pg-multi-notify", "postgres:17-alpine")
	_ = pg

	// Two webhook receivers — one stands in as Slack, one as the generic
	// Webhook notifier. The receiver script writes the body to /last.json
	// so we can inspect it after the run.
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

	startReceiver := func(alias string) testcontainers.Container {
		ctr, err := testcontainers.Run(ctx, "python:3.11-slim",
			testcontainers.WithCmd("python", "-c", receiverPy),
			testcontainers.WithExposedPorts("8088/tcp"),
			tcnetwork.WithNetwork([]string{alias}, sharedNet),
			testcontainers.WithWaitStrategy(wait.ForLog("listening").WithStartupTimeout(30*time.Second)),
		)
		if err != nil {
			t.Fatalf("start receiver %s: %v", alias, err)
		}
		t.Cleanup(func() { _ = ctr.Terminate(ctx) })
		return ctr
	}
	slack := startReceiver("e2e-multi-slack")
	hook := startReceiver("e2e-multi-webhook")

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":              "postgresql",
			"DB_HOST":              "e2e-pg-multi-notify",
			"DB_USER":              "postgres",
			"DB_PASSWORD":          "t",
			"DB_NAME":              "appdb",
			"S3_BUCKET":            bucket,
			"S3_PREFIX":            "multi",
			"SLACK_WEBHOOK_URL":    "http://e2e-multi-slack:8088/slack",
			"SLACK_NOTIFY_SUCCESS": "true",
			"WEBHOOK_URL":          "http://e2e-multi-webhook:8088/hook",
			"WEBHOOK_NOTIFY_SUCCESS": "true",
			"NOTIFY_STDOUT":        "true",
			"NOTIFY_STDOUT_SUCCESS": "true",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	// Slack receiver got hit?
	slackBody := runWithinContainer(t, slack, "cat", "/last.json")
	if slackBody == "" {
		t.Errorf("Slack receiver got no payload")
	}
	// Webhook receiver got hit?
	hookBody := runWithinContainer(t, hook, "cat", "/last.json")
	if hookBody == "" {
		t.Errorf("Webhook receiver got no payload")
	}
	// Stdout notifier emits a JSON line per event — payload has
	// {"event":"success",…}.
	if !strings.Contains(logs, `"event":"success"`) {
		t.Errorf("stdout notifier didn't emit a success event in logs:\n%s", logs)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Anonymous Redis (no DB_USER) — dumpscript explicitly allows this
// ────────────────────────────────────────────────────────────────────────────

func TestAnonymousRedis(t *testing.T) {
	bucket := "anon-redis-e2e"
	createBucket(t, bucket)

	ctx := context.Background()
	r, err := testcontainers.Run(ctx, "redis:7-alpine",
		testcontainers.WithCmd("redis-server"),
		testcontainers.WithExposedPorts("6379/tcp"),
		tcnetwork.WithNetwork([]string{"e2e-redis-anon"}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Ready to accept connections").WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	defer func() { _ = r.Terminate(ctx) }()

	// No DB_USER, no DB_PASSWORD — this is the anonymous path the binary
	// explicitly allows for redis/etcd/elasticsearch.
	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":   "redis",
			"DB_HOST":   "e2e-redis-anon",
			"S3_BUCKET": bucket,
			"S3_PREFIX": "anon-redis",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	if firstKeyWithSuffix(listKeys(t, bucket), ".rdb.gz") == "" {
		t.Fatalf("no .rdb.gz found; keys=%v", listKeys(t, bucket))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Anonymous etcd
// ────────────────────────────────────────────────────────────────────────────

func TestAnonymousEtcd(t *testing.T) {
	bucket := "anon-etcd-e2e"
	createBucket(t, bucket)

	ctx := context.Background()
	e, err := testcontainers.Run(ctx, "quay.io/coreos/etcd:v3.5.10",
		testcontainers.WithEnv(map[string]string{
			"ETCDCTL_API":                  "3",
			"ETCD_LISTEN_CLIENT_URLS":      "http://0.0.0.0:2379",
			"ETCD_ADVERTISE_CLIENT_URLS":   "http://e2e-etcd-anon:2379",
			"ETCD_LISTEN_PEER_URLS":        "http://0.0.0.0:2380",
			"ETCD_INITIAL_ADVERTISE_PEER_URLS": "http://e2e-etcd-anon:2380",
			"ETCD_INITIAL_CLUSTER":         "default=http://e2e-etcd-anon:2380",
			"ETCD_DATA_DIR":                "/etcd-data",
		}),
		testcontainers.WithExposedPorts("2379/tcp"),
		tcnetwork.WithNetwork([]string{"e2e-etcd-anon"}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForLog("ready to serve client requests").WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("start etcd: %v", err)
	}
	defer func() { _ = e.Terminate(ctx) }()

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":   "etcd",
			"DB_HOST":   "e2e-etcd-anon",
			"S3_BUCKET": bucket,
			"S3_PREFIX": "anon-etcd",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	if firstKeyWithSuffix(listKeys(t, bucket), ".db.gz") == "" {
		t.Fatalf("no .db.gz found; keys=%v", listKeys(t, bucket))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Concurrent dumpscript runs — exactly one succeeds, others skip
// ────────────────────────────────────────────────────────────────────────────

func TestConcurrentDumps(t *testing.T) {
	bucket := "concurrent-e2e"
	createBucket(t, bucket)
	pg := startPostgres(t, "e2e-pg-concurrent", "postgres:17-alpine")
	_ = pg

	const N = 3
	type result struct {
		exit int
		logs string
	}
	results := make([]result, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			logs, exit := runDumpscript(t, dumpscriptRun{
				Subcmd: "dump",
				Env: map[string]string{
					"DB_TYPE":     "postgresql",
					"DB_HOST":     "e2e-pg-concurrent",
					"DB_USER":     "postgres",
					"DB_PASSWORD": "t",
					"DB_NAME":     "appdb",
					"S3_BUCKET":   bucket,
					"S3_PREFIX":   "concurrent",
				},
			})
			results[idx] = result{exit, logs}
		}(i)
	}
	wg.Wait()

	// All should exit 0: one runs the actual dump, others see the lock and
	// skip cleanly (EventSkipped).
	for i, r := range results {
		if r.exit != 0 {
			t.Errorf("run %d exited %d (expected 0 — even skipped runs exit clean)\nlogs:\n%s",
				i, r.exit, r.logs)
		}
	}
	// Exactly one .sql.gz should be in S3.
	dumps := 0
	for _, k := range listKeys(t, bucket) {
		if strings.HasSuffix(k, ".sql.gz") &&
			strings.HasPrefix(k, "concurrent/") {
			dumps++
		}
	}
	if dumps != 1 {
		t.Errorf("expected exactly 1 dump in S3, got %d", dumps)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Storage chunk size override (verify upload still succeeds at unusual size)
// ────────────────────────────────────────────────────────────────────────────

func TestStorageChunkSizeOverride(t *testing.T) {
	bucket := "chunk-size-e2e"
	createBucket(t, bucket)
	pg := startPostgres(t, "e2e-pg-chunk", "postgres:17-alpine")
	_ = pg

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":                    "postgresql",
			"DB_HOST":                    "e2e-pg-chunk",
			"DB_USER":                    "postgres",
			"DB_PASSWORD":                "t",
			"DB_NAME":                    "appdb",
			"S3_BUCKET":                  bucket,
			"S3_PREFIX":                  "chunk",
			"STORAGE_CHUNK_SIZE":         "5M",
			"STORAGE_UPLOAD_CONCURRENCY": "1",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)
	if firstKeyWithSuffix(listKeys(t, bucket), ".sql.gz") == "" {
		t.Fatalf("no upload happened with custom chunk-size config")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// LogFormat=console and LogLevel=debug produce different output shapes
// ────────────────────────────────────────────────────────────────────────────

func TestLogFormatAndLevel(t *testing.T) {
	bucket := "log-config-e2e"
	createBucket(t, bucket)
	pg := startPostgres(t, "e2e-pg-log", "postgres:17-alpine")
	_ = pg

	t.Run("console format produces non-JSON lines", func(t *testing.T) {
		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "dump",
			Env: map[string]string{
				"DB_TYPE":     "postgresql",
				"DB_HOST":     "e2e-pg-log",
				"DB_USER":     "postgres",
				"DB_PASSWORD": "t",
				"DB_NAME":     "appdb",
				"S3_BUCKET":   bucket,
				"S3_PREFIX":   "log-console",
				"LOG_FORMAT":  "console",
			},
		})
		assertDumpscriptOK(t, "dump", logs, exit)
		if strings.Contains(logs, `"time":`) && strings.Contains(logs, `"level":`) {
			t.Errorf("console format should not produce JSON lines:\n%s", logs[:min(len(logs), 500)])
		}
	})

	t.Run("debug level shows debug-rated messages", func(t *testing.T) {
		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "dump",
			Env: map[string]string{
				"DB_TYPE":     "postgresql",
				"DB_HOST":     "e2e-pg-log",
				"DB_USER":     "postgres",
				"DB_PASSWORD": "t",
				"DB_NAME":     "appdb",
				"S3_BUCKET":   bucket,
				"S3_PREFIX":   "log-debug",
				"LOG_LEVEL":   "debug",
			},
		})
		assertDumpscriptOK(t, "dump", logs, exit)
		if !strings.Contains(logs, "DEBUG") && !strings.Contains(logs, `"level":"DEBUG"`) {
			t.Errorf("debug level should produce DEBUG-rated lines:\n%s", logs[:min(len(logs), 500)])
		}
	})
}

// ────────────────────────────────────────────────────────────────────────────
// helpers
// ────────────────────────────────────────────────────────────────────────────

func containsKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}
