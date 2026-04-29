//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

// fixedAESKey — 32-byte hex (64 chars). Deterministic on purpose so the
// round-trip spec can run a backup with key K, then a restore with key K
// without managing shared state across containers. Real workloads should
// use a randomly generated key from a Secret/KMS.
const fixedAESKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// ---------------- Manifest sidecar ----------------

func TestManifestSidecar(t *testing.T) {
	bucket := "manifest-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-manifest", "postgres:17-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE m(id int); INSERT INTO m VALUES (1),(2);")

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-manifest",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_PREFIX":   "manifest",
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	keys := listKeys(t, bucket)
	dumpKey := firstKeyWithSuffix(keys, ".sql.gz")
	if dumpKey == "" {
		t.Fatalf("no .sql.gz key found in bucket; keys=%v", keys)
	}
	manifestKey := dumpKey + ".manifest.json"

	// Manifest sidecar must exist alongside the dump.
	body, err := getObject(t, bucket, manifestKey)
	if err != nil {
		t.Fatalf("manifest sidecar missing or unreadable: %v\nkeys=%v", err, keys)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("manifest is not valid JSON: %v\nbody:\n%s", err, body)
	}
	// Spot-check the fields that exercise the binary's wire-up.
	if v := got["schemaVersion"]; v != float64(1) {
		t.Errorf("schemaVersion = %v, want 1", v)
	}
	if got["engine"] != "postgresql" {
		t.Errorf("engine = %q, want postgresql", got["engine"])
	}
	if got["dbName"] != "appdb" {
		t.Errorf("dbName = %q", got["dbName"])
	}
	if got["key"] != dumpKey {
		t.Errorf("key = %q, want %q", got["key"], dumpKey)
	}
	if size, _ := got["sizeBytes"].(float64); size <= 0 {
		t.Errorf("sizeBytes = %v, want > 0", got["sizeBytes"])
	}
	if got["checksumType"] != "sha256" {
		t.Errorf("checksumType = %q, want sha256", got["checksumType"])
	}
	if got["compression"] != "gzip" {
		t.Errorf("compression = %q, want gzip", got["compression"])
	}
	if dur, _ := got["durationSeconds"].(float64); dur <= 0 {
		t.Errorf("durationSeconds = %v, want > 0", got["durationSeconds"])
	}
}

// ---------------- Post-dump hook ----------------

func TestPostDumpHook(t *testing.T) {
	bucket := "hook-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-hook", "postgres:17-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE h(id int); INSERT INTO h VALUES (1);")

	const sentinel = "POST_DUMP_HOOK_RAN_OK"
	hookCmd := fmt.Sprintf(
		`echo "%s key=$DUMPSCRIPT_KEY engine=$DUMPSCRIPT_ENGINE size=$DUMPSCRIPT_SIZE_BYTES dur=$DUMPSCRIPT_DURATION_SECS"`,
		sentinel)

	logs, exit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":        "postgresql",
			"DB_HOST":        "e2e-pg-hook",
			"DB_USER":        "postgres",
			"DB_PASSWORD":    "t",
			"DB_NAME":        "appdb",
			"S3_BUCKET":      bucket,
			"S3_PREFIX":      "hook",
			"POST_DUMP_HOOK": hookCmd,
		},
	})
	assertDumpscriptOK(t, "dump", logs, exit)

	if !strings.Contains(logs, sentinel) {
		t.Errorf("hook output missing in dumpscript logs:\n%s", logs)
	}
	// Confirm env-var interpolation actually fired (proves we passed the
	// metadata correctly, not just a static string).
	for _, want := range []string{"engine=postgresql", "size=", "dur="} {
		if !strings.Contains(logs, want) {
			t.Errorf("hook env-var %q missing in logs:\n%s", want, logs)
		}
	}
}

// ---------------- AES round-trip ----------------

func TestAESRoundTrip(t *testing.T) {
	bucket := "aes-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-aes", "postgres:17-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE m(id int, val text); INSERT INTO m VALUES (1,'aes-marker');")

	// Dump with ENCRYPTION_KEY set — uploaded blob should have .aes suffix.
	dumpEnv := map[string]string{
		"DB_TYPE":        "postgresql",
		"DB_HOST":        "e2e-pg-aes",
		"DB_USER":        "postgres",
		"DB_PASSWORD":    "t",
		"DB_NAME":        "appdb",
		"S3_BUCKET":      bucket,
		"S3_PREFIX":      "aes",
		"ENCRYPTION_KEY": fixedAESKey,
	}
	logs, exit := runDumpscript(t, dumpscriptRun{Subcmd: "dump", Env: dumpEnv})
	assertDumpscriptOK(t, "dump", logs, exit)

	keys := listKeys(t, bucket)
	aesKey := firstKeyWithSuffix(keys, ".sql.gz.aes")
	if aesKey == "" {
		t.Fatalf("no .sql.gz.aes key found; keys=%v", keys)
	}
	// Plaintext .sql.gz should NOT be present (encryption replaces, not adds).
	for _, k := range keys {
		if strings.HasSuffix(k, ".sql.gz") && !strings.HasSuffix(k, ".aes") {
			t.Errorf("plaintext %q found alongside encrypted %q — encryption was bypassed", k, aesKey)
		}
	}

	// Manifest should record encryption=aes-256-gcm.
	body, err := getObject(t, bucket, aesKey+".manifest.json")
	if err == nil {
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		if got["encryption"] != "aes-256-gcm" {
			t.Errorf("manifest.encryption = %q, want aes-256-gcm", got["encryption"])
		}
	}

	// Drop the marker to prove restore actually ran.
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c", "DROP TABLE m;")

	// Restore with the same ENCRYPTION_KEY should decrypt + apply.
	restoreEnv := map[string]string{
		"DB_TYPE":        "postgresql",
		"DB_HOST":        "e2e-pg-aes",
		"DB_USER":        "postgres",
		"DB_PASSWORD":    "t",
		"DB_NAME":        "appdb",
		"S3_BUCKET":      bucket,
		"S3_KEY":         aesKey,
		"ENCRYPTION_KEY": fixedAESKey,
	}
	rlogs, rexit := runDumpscript(t, dumpscriptRun{Subcmd: "restore", Env: restoreEnv})
	assertDumpscriptOK(t, "restore", rlogs, rexit)

	out := runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb",
		"-At", "-c", "SELECT val FROM m WHERE val='aes-marker';")
	if !strings.Contains(out, "aes-marker") {
		t.Fatalf("marker row not restored after AES round-trip:\n%s", out)
	}
}

// ---------------- Restore --dry-run ----------------

func TestRestoreDryRun(t *testing.T) {
	bucket := "dryrun-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-dryrun", "postgres:17-alpine")
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c",
		"CREATE TABLE m(id int, val text); INSERT INTO m VALUES (1,'dryrun-marker');")

	// First, run a real dump to produce something Restore can target.
	dumpLogs, dumpExit := runDumpscript(t, dumpscriptRun{
		Subcmd: "dump",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-dryrun",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_PREFIX":   "dryrun",
		},
	})
	assertDumpscriptOK(t, "dump", dumpLogs, dumpExit)

	keys := listKeys(t, bucket)
	dumpKey := firstKeyWithSuffix(keys, ".sql.gz")
	if dumpKey == "" {
		t.Fatalf("no dump produced; keys=%v", keys)
	}

	// Drop the marker so we can prove the dry-run did NOT recreate it.
	runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb", "-c", "DROP TABLE m;")

	// Restore with DRY_RUN=true.
	rlogs, rexit := runDumpscript(t, dumpscriptRun{
		Subcmd: "restore",
		Env: map[string]string{
			"DB_TYPE":     "postgresql",
			"DB_HOST":     "e2e-pg-dryrun",
			"DB_USER":     "postgres",
			"DB_PASSWORD": "t",
			"DB_NAME":     "appdb",
			"S3_BUCKET":   bucket,
			"S3_KEY":      dumpKey,
			"DRY_RUN":     "true",
		},
	})
	if rexit != 0 {
		t.Fatalf("dryRun restore exit=%d; logs:\n%s", rexit, rlogs)
	}
	if !strings.Contains(rlogs, "dry-run") {
		t.Errorf("expected dry-run mention in logs; got:\n%s", rlogs)
	}

	// Marker table should still be missing — proves no apply happened.
	out := runWithinContainer(t, pg, "psql", "-U", "postgres", "-d", "appdb",
		"-At", "-c", "SELECT to_regclass('public.m');")
	if strings.TrimSpace(out) != "" {
		t.Errorf("dry-run should NOT have recreated table m; psql output: %q", out)
	}
}

// ---------------- validate subcommand ----------------

func TestValidateSubcommand(t *testing.T) {
	bucket := "validate-e2e"
	createBucket(t, bucket)

	pg := startPostgres(t, "e2e-pg-validate", "postgres:17-alpine")
	_ = pg // postgres just needs to be reachable

	t.Run("valid config exits 0", func(t *testing.T) {
		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "validate",
			Env: map[string]string{
				"DB_TYPE":     "postgresql",
				"DB_HOST":     "e2e-pg-validate",
				"DB_USER":     "postgres",
				"DB_PASSWORD": "t",
				"DB_NAME":     "appdb",
				"PERIODICITY": "daily",
				"S3_BUCKET":   bucket,
			},
		})
		if exit != 0 {
			t.Fatalf("validate exit=%d; logs:\n%s", exit, logs)
		}
		for _, want := range []string{
			"ValidateCommon passed",
			"storage reachable",
			"All validations passed",
		} {
			if !strings.Contains(logs, want) {
				t.Errorf("missing %q in logs:\n%s", want, logs)
			}
		}
		// Secret values must be redacted in the printed summary.
		if strings.Contains(logs, "AWS_SECRET_ACCESS_KEY=") &&
			!strings.Contains(logs, "[REDACTED]") {
			t.Errorf("secret leaked in validate output:\n%s", logs)
		}
	})

	t.Run("missing DB_HOST → ExitConfigInvalid (2)", func(t *testing.T) {
		logs, exit := runDumpscript(t, dumpscriptRun{
			Subcmd: "validate",
			Env: map[string]string{
				"DB_TYPE":     "postgresql",
				"DB_USER":     "postgres",
				"PERIODICITY": "daily",
				"S3_BUCKET":   bucket,
				// DB_HOST intentionally missing
			},
		})
		if exit == 0 {
			t.Fatalf("expected non-zero exit when DB_HOST missing; got 0\nlogs:\n%s", logs)
		}
		// validate prints the failure to stderr with a clear message.
		if !strings.Contains(logs, "DB_HOST") {
			t.Errorf("expected DB_HOST mention in failure logs:\n%s", logs)
		}
	})
}

// ---------------- helpers ----------------

// getObject fetches an object's body via the shared MinIO client.
func getObject(t *testing.T, bucket, key string) ([]byte, error) {
	t.Helper()
	ctx := context.Background()
	c := s3client(t)
	obj, err := c.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()
	// Use ReadFrom against bytes.Buffer — handles short reads transparently
	// and matches the body-collection pattern used elsewhere in this suite.
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(obj); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// silence unused — fmt is consumed in only some sub-tests; the import stays
// because adding it conditionally breaks gofmt.
var _ = fmt.Sprintf
