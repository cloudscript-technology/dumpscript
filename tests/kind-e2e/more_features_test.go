//go:build kind_e2e

package kinde2e_test

// more_features_test.go covers binary features added after the original
// features_test.go landed: manifest sidecar JSON, Restore dry-run, engine
// sub-block env injection, post-dump hook execution, and AES round-trip.

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// fixedTestKey is a 64-char hex (32 bytes) used by the encryption spec. Not
// derived from /dev/urandom on purpose: we want deterministic key material so
// the spec can verify both encrypt + decrypt round-trip without managing
// shared state across BackupSchedule / Restore CRs.
const fixedTestKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var _ = Describe("Manifest sidecar", Ordered, func() {
	const (
		name   = "manifest-e2e"
		prefix = "manifest-e2e"
		job    = "manifest-e2e-manual"
	)

	schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: %s
  namespace: %s
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  notifications:
    stdout: true
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef:
      name: postgres-credentials
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: "%s"
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, prefix, localstackInCluster)

	BeforeAll(func() {
		applyManifest(schedule)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("uploads <key>.manifest.json alongside the dump with the expected fields", func() {
		By("triggering a manual Job")
		run("kubectl", "create", "job", job,
			"--from=cronjob/"+name, "-n", testNamespace)
		Eventually(func() string {
			complete, _ := runOutput("kubectl", "get", "job", job,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return complete
		}, 5*time.Minute, 3*time.Second).Should(Equal("True"))

		By("locating the dump key")
		var dumpKey string
		Eventually(func() string {
			objects, _ := listS3Objects(bucketName)
			for _, k := range objects {
				if strings.HasPrefix(k, prefix+"/daily/") &&
					(strings.HasSuffix(k, ".gz") || strings.HasSuffix(k, ".zst")) {
					dumpKey = k
					return k
				}
			}
			return ""
		}, 30*time.Second, 2*time.Second).ShouldNot(BeEmpty())

		By("downloading the .manifest.json sidecar via LocalStack HTTP")
		manifestKey := dumpKey + ".manifest.json"
		var body []byte
		Eventually(func() error {
			url := "http://localhost:" + lsLocalPort + "/" + bucketName + "/" + manifestKey
			resp, err := http.Get(url) //nolint:noctx
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 400 {
				return fmt.Errorf("HTTP %d on %s", resp.StatusCode, manifestKey)
			}
			b := make([]byte, 0, 1024)
			buf := make([]byte, 512)
			for {
				n, _ := resp.Body.Read(buf)
				if n == 0 {
					break
				}
				b = append(b, buf[:n]...)
			}
			body = b
			return nil
		}, 30*time.Second, 2*time.Second).Should(Succeed())

		By("parsing JSON and asserting the run metadata is captured")
		var got map[string]any
		Expect(json.Unmarshal(body, &got)).To(Succeed(), "manifest body:\n%s", body)
		Expect(got["schemaVersion"]).To(BeNumerically("==", 1))
		Expect(got["engine"]).To(Equal("postgresql"))
		Expect(got["dbName"]).To(Equal("testdb"))
		Expect(got["periodicity"]).To(Equal("daily"))
		Expect(got["key"]).To(Equal(dumpKey))
		Expect(got["sizeBytes"]).To(BeNumerically(">", 0))
		Expect(got["checksum"]).NotTo(BeEmpty())
		Expect(got["checksumType"]).To(Equal("sha256"))
		Expect(got["compression"]).To(Equal("gzip"))
		Expect(got["durationSeconds"]).To(BeNumerically(">", 0))
	})
})

var _ = Describe("Engine sub-block env injection", func() {
	// Apply two BackupSchedules with engine sub-blocks set, verify the
	// generated CronJob env contains the translated DUMP_OPTIONS the
	// operator's mongoExtras() helper produces. Picks redis (db + tls) and
	// sqlserver (trustServerCertificate) as representative samples — the
	// other 6 engines use the same translation path.

	type subBlockCase struct {
		name         string
		crName       string
		manifest     string
		expectInOpts []string
	}

	cases := []subBlockCase{
		{
			name:   "redis db + tls → -n + --tls in DUMP_OPTIONS",
			crName: "redis-subblock-e2e",
			manifest: fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata: { name: redis-subblock-e2e, namespace: %s }
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  database:
    type: redis
    host: redis.%s.svc.cluster.local
    redis:
      db: 5
      tls: true
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: redis-subblock-e2e
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
`, testNamespace, dumpscriptImg, testNamespace, bucketName, localstackInCluster),
			expectInOpts: []string{"-n", "5", "--tls"},
		},
		{
			name:   "sqlserver trustServerCertificate → -W in DUMP_OPTIONS",
			crName: "sqlserver-subblock-e2e",
			manifest: fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata: { name: sqlserver-subblock-e2e, namespace: %s }
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  database:
    type: sqlserver
    host: mssql.%s.svc.cluster.local
    name: appdb
    credentialsSecretRef: { name: postgres-credentials }
    sqlserver:
      trustServerCertificate: true
      applicationIntent: ReadOnly
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: sqlserver-subblock-e2e
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
`, testNamespace, dumpscriptImg, testNamespace, bucketName, localstackInCluster),
			expectInOpts: []string{"-W", "--application-intent=ReadOnly"},
		},
	}

	for _, tc := range cases {
		tc := tc // pin
		It(tc.name, func() {
			applyManifest(tc.manifest)
			defer runOutput("kubectl", "delete", "backupschedule", tc.crName, //nolint:errcheck
				"-n", testNamespace, "--ignore-not-found")

			Eventually(func() string {
				out, _ := runOutput("kubectl", "get", "cronjob", tc.crName,
					"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
				return out
			}).Should(Equal(tc.crName))

			out, err := runOutput("kubectl", "get", "cronjob", tc.crName,
				"-n", testNamespace,
				"-o", `jsonpath={range .spec.jobTemplate.spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}`)
			Expect(err).NotTo(HaveOccurred())
			for _, expect := range tc.expectInOpts {
				Expect(out).To(ContainSubstring(expect),
					"DUMP_OPTIONS missing %q in env:\n%s", expect, out)
			}
		})
	}
})

var _ = Describe("Post-dump hook", Ordered, func() {
	const (
		name   = "hook-e2e"
		prefix = "hook-e2e"
		job    = "hook-e2e-manual"
		// Hook writes a sentinel string to stdout; we grep pod logs for it.
		// The hook is a single shell command from the operator's ExtraEnv.
		hookOutput = "POST_DUMP_HOOK_RAN_OK"
	)

	schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: %s
  namespace: %s
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  notifications:
    stdout: true
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef:
      name: postgres-credentials
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: "%s"
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
  extraEnv:
    - name: POST_DUMP_HOOK
      value: 'echo "%s key=$DUMPSCRIPT_KEY engine=$DUMPSCRIPT_ENGINE size=$DUMPSCRIPT_SIZE_BYTES"'
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, prefix, localstackInCluster, hookOutput)

	BeforeAll(func() {
		applyManifest(schedule)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("runs the hook with DUMPSCRIPT_* env vars after upload", func() {
		run("kubectl", "create", "job", job,
			"--from=cronjob/"+name, "-n", testNamespace)
		Eventually(func() string {
			complete, _ := runOutput("kubectl", "get", "job", job,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return complete
		}, 5*time.Minute, 3*time.Second).Should(Equal("True"))

		// Pod logs include the hook's stdout (the binary redirects it to
		// stderr so kubectl logs picks it up).
		var logs string
		Eventually(func() string {
			out, err := runOutput("kubectl", "logs",
				"-l", "job-name="+job, "-n", testNamespace, "--tail=200")
			if err != nil {
				return ""
			}
			logs = out
			return out
		}, 60*time.Second, 3*time.Second).Should(ContainSubstring(hookOutput))

		// Sanity: the hook's env-var interpolation actually fired.
		Expect(logs).To(ContainSubstring("engine=postgresql"))
		Expect(logs).To(MatchRegexp(`size=\d+`))
	})
})

var _ = Describe("AES-256-GCM encryption round-trip", Ordered, func() {
	const (
		name        = "aes-e2e"
		prefix      = "aes-e2e"
		job         = "aes-e2e-manual"
		restoreName = "aes-restore-e2e"
	)

	var backupKey string

	schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: %s
  namespace: %s
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  notifications:
    stdout: true
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef:
      name: postgres-credentials
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: "%s"
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
  extraEnv:
    - name: ENCRYPTION_KEY
      value: "%s"
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, prefix, localstackInCluster, fixedTestKey)

	BeforeAll(func() {
		// Ensure the marker exists so the round-trip can verify post-restore.
		psql("CREATE TABLE IF NOT EXISTS aes_marker (id SERIAL PRIMARY KEY, val TEXT);")
		psql("TRUNCATE aes_marker;")
		psql("INSERT INTO aes_marker (val) VALUES ('aes-marker');")

		applyManifest(schedule)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
		runOutput("kubectl", "delete", "restore", restoreName, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("the uploaded key has the .aes suffix (encrypted blob, not gzip)", func() {
		// Sanity: the test-side key is well-formed before we ask the binary
		// to consume it.
		_, err := hex.DecodeString(fixedTestKey)
		Expect(err).NotTo(HaveOccurred(), "test key must be valid hex")

		run("kubectl", "create", "job", job,
			"--from=cronjob/"+name, "-n", testNamespace)
		Eventually(func() (string, error) {
			complete, err := runOutput("kubectl", "get", "job", job,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			if err != nil {
				return "", err
			}
			if complete == "True" {
				return "Complete", nil
			}
			failed, _ := runOutput("kubectl", "get", "job", job,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Failed")].status}`)
			if failed == "True" {
				logs, _ := runOutput("kubectl", "logs",
					"-l", "job-name="+job, "-n", testNamespace, "--tail=80")
				return "Failed", fmt.Errorf("encrypted backup job failed:\n%s", logs)
			}
			return "", nil
		}, 5*time.Minute, 3*time.Second).Should(Equal("Complete"))

		Eventually(func() ([]string, error) {
			return listS3Objects(bucketName)
		}).Should(Not(BeEmpty()))
		objects, _ := listS3Objects(bucketName)
		var foundAes, foundPlain bool
		for _, k := range objects {
			if !strings.HasPrefix(k, prefix+"/") {
				continue
			}
			if strings.HasSuffix(k, ".gz.aes") || strings.HasSuffix(k, ".zst.aes") {
				foundAes = true
				backupKey = k
			}
			if strings.HasSuffix(k, ".gz") || strings.HasSuffix(k, ".zst") {
				if !strings.HasSuffix(k, ".aes") {
					foundPlain = true
				}
			}
		}
		Expect(foundAes).To(BeTrue(),
			"expected at least one .aes object under %s/, got: %v", prefix, objects)
		Expect(foundPlain).To(BeFalse(),
			"plaintext .gz/.zst found alongside .aes — encryption was bypassed: %v", objects)
	})

	It("the manifest records encryption=aes-256-gcm", func() {
		Expect(backupKey).NotTo(BeEmpty(), "backupKey must be set by the preceding spec")
		manifestKey := backupKey + ".manifest.json"
		url := "http://localhost:" + lsLocalPort + "/" + bucketName + "/" + manifestKey
		resp, err := http.Get(url) //nolint:noctx
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(BeNumerically("<", 400))
		body := make([]byte, 0, 1024)
		buf := make([]byte, 512)
		for {
			n, _ := resp.Body.Read(buf)
			if n == 0 {
				break
			}
			body = append(body, buf[:n]...)
		}
		var got map[string]any
		Expect(json.Unmarshal(body, &got)).To(Succeed())
		Expect(got["encryption"]).To(Equal("aes-256-gcm"))
	})

	It("Restore with the same ENCRYPTION_KEY decrypts and recovers the data", func() {
		Expect(backupKey).NotTo(BeEmpty())

		psql("DROP TABLE IF EXISTS aes_marker;")

		restore := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: Restore
metadata:
  name: %s
  namespace: %s
spec:
  sourceKey: "%s"
  image: %s
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef:
      name: postgres-credentials
  storage:
    backend: s3
    s3:
      bucket: %s
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
  extraEnv:
    - name: ENCRYPTION_KEY
      value: "%s"
  notifications:
    stdout: true
`, restoreName, testNamespace, backupKey, dumpscriptImg, testNamespace, bucketName, localstackInCluster, fixedTestKey)
		applyManifest(restore)

		Eventually(func() string {
			phase, _ := runOutput("kubectl", "get", "restore", restoreName,
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			return phase
		}, 5*time.Minute, 5*time.Second).Should(Equal("Succeeded"))

		out := psql("SELECT val FROM aes_marker WHERE val = 'aes-marker';")
		Expect(out).To(ContainSubstring("aes-marker"))
	})
})

var _ = Describe("Restore --dry-run", Ordered, func() {
	const restoreName = "dryrun-restore-e2e"

	BeforeAll(func() {
		// dryRun probes the sourceKey via Storage.Exists. Need a real key to
		// exist — reuse anything the main suite already produced. Find one
		// (any postgres backup) before applying the Restore CR.
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "restore", restoreName, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("a dryRun=true Restore CR reaches Succeeded without applying anything", func() {
		var existingKey string
		// dryRun only needs Storage.Exists to return true — accept ANY postgres
		// backup any prior spec has uploaded so we are not coupled to the order
		// in which Ordered containers run (Ginkgo randomize-all otherwise).
		Eventually(func() string {
			objects, err := listS3Objects(bucketName)
			if err != nil {
				return ""
			}
			for _, k := range objects {
				if strings.Contains(k, "/daily/") &&
					(strings.HasSuffix(k, ".gz") || strings.HasSuffix(k, ".zst")) &&
					!strings.HasSuffix(k, ".manifest.json") {
					existingKey = k
					return k
				}
			}
			return ""
		}, 5*time.Minute, 5*time.Second).ShouldNot(BeEmpty(),
			"this spec needs any postgres/etc backup .gz/.zst to exist as sourceKey")

		// Drop the marker so that an *actually-applied* restore would
		// recreate it. dryRun should NOT recreate it.
		psql("DROP TABLE IF EXISTS e2e_marker;")

		restore := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: Restore
metadata:
  name: %s
  namespace: %s
spec:
  sourceKey: "%s"
  image: %s
  dryRun: true
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef:
      name: postgres-credentials
  storage:
    backend: s3
    s3:
      bucket: %s
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
  notifications:
    stdout: true
`, restoreName, testNamespace, existingKey, dumpscriptImg, testNamespace, bucketName, localstackInCluster)
		applyManifest(restore)

		Eventually(func() string {
			phase, _ := runOutput("kubectl", "get", "restore", restoreName,
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			return phase
		}, 3*time.Minute, 5*time.Second).Should(Equal("Succeeded"))

		// Verify dryRun did NOT apply the dump: e2e_marker should still
		// be missing because we dropped it and the dryRun didn't run the
		// real restorer.
		out := psql("SELECT to_regclass('public.e2e_marker');")
		// to_regclass returns NULL when the table doesn't exist; the trimmed
		// output is empty for NULL.
		Expect(strings.TrimSpace(out)).To(BeEmpty(),
			"dryRun should NOT have recreated e2e_marker; psql output: %q", out)
	})
})
