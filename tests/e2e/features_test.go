//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// ---------------- Azure via Azurite ----------------

// azuriteKey is Azurite's well-known devstoreaccount1 key (published by
// Microsoft; safe for local emulator testing only).
const azuriteKey = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="

func TestAzure(t *testing.T) {
	ctx := context.Background()
	azAlias := "e2e-azurite"

	az, err := testcontainers.Run(ctx, "mcr.microsoft.com/azure-storage/azurite:latest",
		testcontainers.WithCmd("azurite-blob", "--blobHost", "0.0.0.0", "--loose", "--skipApiVersionCheck"),
		testcontainers.WithExposedPorts("10000/tcp"),
		tcnetwork.WithNetwork([]string{azAlias}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForLog("Azurite Blob service successfully listens").
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start azurite: %v", err)
	}
	defer func() { _ = az.Terminate(ctx) }()

	connStr := fmt.Sprintf(
		"DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=%s;BlobEndpoint=http://%s:10000/devstoreaccount1;",
		azuriteKey, azAlias,
	)

	cli, err := testcontainers.Run(ctx, "mcr.microsoft.com/azure-cli:latest",
		testcontainers.WithEnv(map[string]string{"AZURE_STORAGE_CONNECTION_STRING": connStr}),
		testcontainers.WithCmd("az", "storage", "container", "create", "--name", "backups"),
		tcnetwork.WithNetwork(nil, sharedNet),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start az cli helper: %v", err)
	}
	state, _ := cli.State(ctx)
	if state.ExitCode != 0 {
		rc, _ := cli.Logs(ctx)
		b, _ := io.ReadAll(rc)
		_ = cli.Terminate(ctx)
		t.Fatalf("az storage container create failed: %s", b)
	}
	_ = cli.Terminate(ctx)

	pgAlias := "e2e-pg-azure"
	pg := startPostgres(t, pgAlias, "postgres:16-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE t(id int); INSERT INTO t VALUES (1),(2),(3);")

	ctr, err := testcontainers.Run(ctx, dumpscriptImage,
		testcontainers.WithEnv(map[string]string{
			"STORAGE_BACKEND":         "azure",
			"AZURE_STORAGE_ACCOUNT":   "devstoreaccount1",
			"AZURE_STORAGE_KEY":       azuriteKey,
			"AZURE_STORAGE_CONTAINER": "backups",
			"AZURE_STORAGE_PREFIX":    "pg",
			"AZURE_STORAGE_ENDPOINT":  fmt.Sprintf("http://%s:10000/devstoreaccount1", azAlias),
			"PERIODICITY":             "daily",
			"RETENTION_DAYS":          "7",
			"LOG_LEVEL":               "info",
			"DB_TYPE":                 "postgresql",
			"DB_HOST":                 pgAlias,
			"DB_PORT":                 "5432",
			"DB_USER":                 "postgres",
			"DB_PASSWORD":             "t",
			"DB_NAME":                 "appdb",
		}),
		testcontainers.WithCmd("dump"),
		tcnetwork.WithNetwork(nil, sharedNet),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(120*time.Second)),
	)
	if err != nil {
		t.Fatalf("start azure dump: %v", err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	s, err := ctr.State(ctx)
	if err != nil {
		t.Fatalf("container state: %v", err)
	}
	if s.ExitCode != 0 {
		rc, _ := ctr.Logs(ctx)
		b, _ := io.ReadAll(rc)
		t.Fatalf("azure dump exit=%d; logs:\n%s", s.ExitCode, b)
	}

	listCli, err := testcontainers.Run(ctx, "mcr.microsoft.com/azure-cli:latest",
		testcontainers.WithEnv(map[string]string{"AZURE_STORAGE_CONNECTION_STRING": connStr}),
		testcontainers.WithCmd("az", "storage", "blob", "list",
			"--container-name", "backups", "--query", "[].name", "-o", "tsv"),
		tcnetwork.WithNetwork(nil, sharedNet),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start az list: %v", err)
	}
	defer func() { _ = listCli.Terminate(ctx) }()
	rc, _ := listCli.Logs(ctx)
	blobs, _ := io.ReadAll(rc)
	if !strings.Contains(string(blobs), ".sql.gz") {
		t.Fatalf("azure blob not found; listing:\n%s", blobs)
	}
}

// ---------------- Lock contention ----------------

func TestLockContention(t *testing.T) {
	pgAlias := "e2e-pg-lock"
	pg := startPostgres(t, pgAlias, "postgres:16-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE t(x int); INSERT INTO t VALUES (1);")

	bucket := "lock-test"
	createBucket(t, bucket)

	today := time.Now().UTC().Format("2006/01/02")
	lockKey := fmt.Sprintf("lk/daily/%s/.lock", today)
	putObject(t, bucket, lockKey,
		`{"execution_id":"pre-seeded","hostname":"test","pid":1}`)

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE": "postgresql", "DB_HOST": pgAlias, "DB_PORT": "5432",
			"DB_USER": "postgres", "DB_PASSWORD": "t", "DB_NAME": "appdb",
			"S3_BUCKET": bucket, "S3_PREFIX": "lk",
		},
	})

	if code != 0 {
		t.Fatalf("expected exit 0 when locked, got %d; logs:\n%s", code, logs)
	}
	if !strings.Contains(logs, "another backup in progress") {
		t.Fatalf("expected 'another backup in progress' log; got:\n%s", logs)
	}

	keys := listKeys(t, bucket)
	var foundLock, foundDump bool
	for _, k := range keys {
		if strings.HasSuffix(k, ".lock") {
			foundLock = true
		}
		if strings.Contains(k, "dump_") && strings.HasSuffix(k, ".sql.gz") {
			foundDump = true
		}
	}
	if !foundLock {
		t.Fatal("lock was removed (should remain)")
	}
	if foundDump {
		t.Fatalf("dump was uploaded despite lock; keys=%v", keys)
	}
}

// ---------------- Retention cleanup ----------------

func TestRetention(t *testing.T) {
	bucket := "retention-test"
	createBucket(t, bucket)

	today := time.Now().UTC().Format("2006/01/02")
	putObject(t, bucket, "rt/daily/2020/01/01/dump_old1.sql.gz", "fake-1")
	putObject(t, bucket, "rt/daily/2020/02/15/dump_old2.sql.gz", "fake-2")
	putObject(t, bucket, "rt/daily/2020/06/30/dump_old3.archive.gz", "fake-3")
	putObject(t, bucket, "rt/daily/"+today+"/dump_recent.sql.gz", "fresh")

	if n := countDumpObjects(listKeys(t, bucket)); n != 4 {
		t.Fatalf("expected 4 seed dumps, got %d", n)
	}

	logs, code := runDumpscript(t, dumpscriptRun{
		Subcmd: "cleanup",
		Env: map[string]string{
			"DB_TYPE": "postgresql", "DB_HOST": "dummy",
			"DB_USER": "u", "DB_PASSWORD": "p",
			"S3_BUCKET": bucket, "S3_PREFIX": "rt",
		},
	})
	assertDumpscriptOK(t, "cleanup", logs, code)

	keys := listKeys(t, bucket)
	if n := countDumpObjects(keys); n != 1 {
		t.Fatalf("expected 1 surviving dump, got %d; keys=%v", n, keys)
	}
	surv := firstDumpKey(keys)
	if !strings.Contains(surv, today) {
		t.Fatalf("wrong survivor: %s", surv)
	}
}

func countDumpObjects(keys []string) int {
	n := 0
	for _, k := range keys {
		if strings.Contains(k, "dump_") {
			n++
		}
	}
	return n
}

func firstDumpKey(keys []string) string {
	for _, k := range keys {
		if strings.Contains(k, "dump_") {
			return k
		}
	}
	return ""
}

// ---------------- Slack webhook ----------------

func TestSlackNotification(t *testing.T) {
	ctx := context.Background()
	webhookAlias := "e2e-slack"

	receiverPy := `import http.server,sys
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        n=int(self.headers.get("Content-Length",0))
        body=self.rfile.read(n)
        open("/last.json","wb").write(body)
        self.send_response(200);self.end_headers();self.wfile.write(b"ok")
    def log_message(self,*a,**k):pass
print("listening",flush=True)
http.server.HTTPServer(("0.0.0.0",8088),H).serve_forever()`

	webhook, err := testcontainers.Run(ctx, "python:3.11-slim",
		testcontainers.WithCmd("python", "-c", receiverPy),
		testcontainers.WithExposedPorts("8088/tcp"),
		tcnetwork.WithNetwork([]string{webhookAlias}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForLog("listening").WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start webhook: %v", err)
	}
	defer func() { _ = webhook.Terminate(ctx) }()

	logs, _ := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "nonexistent.invalid",
			"DB_USER":     "u",
			"DB_PASSWORD": "p",
			"DB_NAME":     "x",
			"S3_BUCKET":   "lock-test",
			"S3_PREFIX":   "slack",
			"SLACK_WEBHOOK_URL": fmt.Sprintf(
				"http://%s:8088/webhook", webhookAlias),
			"SLACK_CHANNEL":        "#e2e",
			"SLACK_NOTIFY_SUCCESS": "true",
		},
	})
	_ = logs

	time.Sleep(500 * time.Millisecond)
	body := runWithinContainer(t, webhook, "cat", "/last.json")
	if body == "" {
		t.Fatalf("webhook captured no payload; dumpscript logs:\n%s", logs)
	}
	if !strings.Contains(body, `"channel":"#e2e"`) {
		t.Errorf("channel missing in payload: %s", body)
	}
	if !strings.Contains(body, `"color":"danger"`) {
		t.Errorf("expected color=danger (failure); got: %s", body)
	}
}
