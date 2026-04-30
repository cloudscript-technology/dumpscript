//go:build kind_e2e

package kinde2e_test

// coverage_test.go ports tests/e2e scenarios into the operator-driven kind
// suite. These specs validate the CRD-→-CronJob-/-Job translation rather
// than the binary's behavior in isolation: each one applies a CR shape and
// asserts the operator wired it correctly.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// ────────────────────────────────────────────────────────────────────────────
// Periodicity layouts: each spec.periodicity value lands under its own subtree
// ────────────────────────────────────────────────────────────────────────────

var _ = Describe("Periodicity CRD field → S3 prefix layout", func() {
	for _, p := range []string{"daily", "weekly", "monthly", "yearly"} {
		p := p
		It("periodicity="+p+" produces "+p+"/ subtree", func() {
			name := "period-" + p + "-e2e"
			prefix := "period-" + p
			schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata: { name: %s, namespace: %s }
spec:
  schedule: "0 2 * * *"
  periodicity: %s
  image: %s
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef: { name: postgres-credentials }
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: %q
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
`, name, testNamespace, p, dumpscriptImg, testNamespace, bucketName, prefix, localstackInCluster)

			applyManifest(schedule)
			defer runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
				"-n", testNamespace, "--ignore-not-found")

			job := name + "-manual"
			Eventually(func() string {
				out, _ := runOutput("kubectl", "get", "cronjob", name,
					"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
				return out
			}).Should(Equal(name))

			run("kubectl", "create", "job", job,
				"--from=cronjob/"+name, "-n", testNamespace)
			Eventually(func() string {
				complete, _ := runOutput("kubectl", "get", "job", job,
					"-n", testNamespace,
					"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
				return complete
			}, 5*time.Minute, 3*time.Second).Should(Equal("True"))

			runOutput("kubectl", "delete", "job", job, //nolint:errcheck
				"-n", testNamespace, "--ignore-not-found")

			objects, err := listS3Objects(bucketName)
			Expect(err).NotTo(HaveOccurred())
			var found bool
			for _, k := range objects {
				if strings.HasPrefix(k, prefix+"/"+p+"/") {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(),
				"no key under %s/%s/ found; objects=%v", prefix, p, objects)
		})
	}
})

// ────────────────────────────────────────────────────────────────────────────
// Pod scheduling fields (resources / nodeSelector / imagePullPolicy)
// propagate into the produced CronJob's Pod spec
// ────────────────────────────────────────────────────────────────────────────

var _ = Describe("Pod scheduling CRD fields", Ordered, func() {
	const name = "pod-scheduling-e2e"

	schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata: { name: %s, namespace: %s }
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  imagePullPolicy: IfNotPresent
  imagePullSecrets:
    - name: ghcr-pull-secret
  resources:
    requests: { cpu: "10m", memory: "32Mi" }
    limits:   { memory: "256Mi" }
  nodeSelector:
    kubernetes.io/os: linux
  tolerations:
    - { key: dedicated, operator: Equal, value: backup, effect: NoSchedule }
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef: { name: postgres-credentials }
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: pod-scheduling
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, localstackInCluster)

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

	It("resources requests/limits propagate to the container", func() {
		out, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={.spec.jobTemplate.spec.template.spec.containers[0].resources}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("10m"))
		Expect(out).To(ContainSubstring("32Mi"))
		Expect(out).To(ContainSubstring("256Mi"))
	})

	It("imagePullPolicy + imagePullSecrets propagate", func() {
		policy, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={.spec.jobTemplate.spec.template.spec.containers[0].imagePullPolicy}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(policy).To(Equal("IfNotPresent"))

		secrets, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={.spec.jobTemplate.spec.template.spec.imagePullSecrets[*].name}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(secrets).To(ContainSubstring("ghcr-pull-secret"))
	})

	It("nodeSelector + tolerations propagate", func() {
		ns, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={.spec.jobTemplate.spec.template.spec.nodeSelector}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(ns).To(ContainSubstring("kubernetes.io/os"))

		tols, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={.spec.jobTemplate.spec.template.spec.tolerations}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(tols).To(ContainSubstring("dedicated"))
		Expect(tols).To(ContainSubstring("backup"))
	})
})

// ────────────────────────────────────────────────────────────────────────────
// Restore failure modes via CRD: bad sourceKey → phase=Failed; bad creds → Failed
// ────────────────────────────────────────────────────────────────────────────

var _ = Describe("Restore failure modes via CRD", func() {
	It("non-existent sourceKey → phase=Failed", func() {
		const name = "restore-bad-source-e2e"
		restore := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: Restore
metadata: { name: %s, namespace: %s }
spec:
  sourceKey: "definitely/does/not/exist.sql.gz"
  image: %s
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef: { name: postgres-credentials }
  storage:
    backend: s3
    s3:
      bucket: %s
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, localstackInCluster)
		applyManifest(restore)
		defer runOutput("kubectl", "delete", "restore", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")

		Eventually(func() string {
			phase, _ := runOutput("kubectl", "get", "restore", name,
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			return phase
		}, 5*time.Minute, 5*time.Second).Should(Equal("Failed"))

		// .status.message should contain something about the failure.
		msg, _ := runOutput("kubectl", "get", "restore", name,
			"-n", testNamespace, "-o", "jsonpath={.status.message}")
		Expect(msg).NotTo(BeEmpty())
	})

	It("Ready condition flips to False on Failed phase", func() {
		const name = "restore-ready-cond-e2e"
		restore := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: Restore
metadata: { name: %s, namespace: %s }
spec:
  sourceKey: "still/does/not/exist.sql.gz"
  image: %s
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef: { name: postgres-credentials }
  storage:
    backend: s3
    s3:
      bucket: %s
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, localstackInCluster)
		applyManifest(restore)
		defer runOutput("kubectl", "delete", "restore", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "restore", name,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`)
			return out
		}, 5*time.Minute, 5*time.Second).Should(Equal("False"))
	})
})

// ────────────────────────────────────────────────────────────────────────────
// Operator emits LastRunFailed event when a Job fails
// ────────────────────────────────────────────────────────────────────────────

var _ = Describe("Operator events on Job failure", Ordered, func() {
	const name = "events-fail-e2e"

	// BackupSchedule pointed at a non-resolvable host so the dumpscript
	// pod's pg_dump fails reliably (within the dump phase, not preflight,
	// because preflight is storage-side).
	schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata: { name: %s, namespace: %s }
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  database:
    type: postgresql
    host: nonexistent-postgres-for-events
    name: testdb
    credentialsSecretRef: { name: postgres-credentials }
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: events-fail
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
  backoffLimit: 0
  activeDeadlineSeconds: 60
`, name, testNamespace, dumpscriptImg, bucketName, localstackInCluster)

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

	It("emits a LastRunFailed event after the Job fails", func() {
		job := name + "-manual"
		run("kubectl", "create", "job", job,
			"--from=cronjob/"+name, "-n", testNamespace)
		defer runOutput("kubectl", "delete", "job", job, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")

		// Wait for the Job to fail (BackoffLimit=0 + ActiveDeadlineSeconds=60).
		Eventually(func() string {
			failed, _ := runOutput("kubectl", "get", "job", job,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Failed")].status}`)
			return failed
		}, 3*time.Minute, 5*time.Second).Should(Equal("True"))

		// Reconciler watches Jobs by label; eventually it observes the
		// failure and emits the LastRunFailed event on the BackupSchedule.
		Eventually(func() string {
			events, _ := runOutput("kubectl", "get", "events",
				"-n", testNamespace,
				"--field-selector", "involvedObject.kind=BackupSchedule,involvedObject.name="+name,
				"-o", `jsonpath={range .items[*]}{.reason}={.message}{"\n"}{end}`)
			return events
		}, 3*time.Minute, 5*time.Second).Should(ContainSubstring("LastRunFailed"))
	})

	It("status.consecutiveFailures increments and Ready condition turns False", func() {
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "backupschedule", name,
				"-n", testNamespace,
				"-o", "jsonpath={.status.consecutiveFailures}")
			return out
		}, 3*time.Minute, 5*time.Second).ShouldNot(Equal("0"))

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "backupschedule", name,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`)
			return out
		}).Should(Equal("False"))
	})
})

// ────────────────────────────────────────────────────────────────────────────
// VerifyContent and WorkDir CRD fields propagate as env vars
// ────────────────────────────────────────────────────────────────────────────

var _ = Describe("VerifyContent + WorkDir CRD fields", func() {
	It("verifyContent: false + custom workDir lands in the container env", func() {
		const name = "verify-workdir-e2e"
		verifyFalse := false
		schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata: { name: %s, namespace: %s }
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  workDir: /tmp/dumpscript-custom
  verifyContent: %t
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef: { name: postgres-credentials }
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: verify-workdir
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
`, name, testNamespace, dumpscriptImg, verifyFalse, testNamespace, bucketName, localstackInCluster)
		applyManifest(schedule)
		defer runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))

		out, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={range .spec.jobTemplate.spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("VERIFY_CONTENT=false"))
		Expect(out).To(ContainSubstring("WORK_DIR=/tmp/dumpscript-custom"))
	})
})

// ────────────────────────────────────────────────────────────────────────────
// CompressionType + dumpRetry CRD fields end up as env vars
// ────────────────────────────────────────────────────────────────────────────

var _ = Describe("Compression + dumpRetry CRD fields", func() {
	It("spec.compression + spec.dumpRetry inject the right env vars", func() {
		const name = "compression-retry-e2e"
		schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata: { name: %s, namespace: %s }
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  compression: zstd
  dumpRetry:
    maxAttempts: 5
    initialBackoff: 3s
    maxBackoff: 1m
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef: { name: postgres-credentials }
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: compression-retry
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, localstackInCluster)
		applyManifest(schedule)
		defer runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))

		out, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={range .spec.jobTemplate.spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("COMPRESSION_TYPE=zstd"))
		Expect(out).To(ContainSubstring("DUMP_RETRIES=5"))
		Expect(out).To(ContainSubstring("DUMP_RETRY_BACKOFF=3s"))
		Expect(out).To(ContainSubstring("DUMP_RETRY_MAX_BACKOFF=1m0s"))
	})
})

// ────────────────────────────────────────────────────────────────────────────
// Status enrichment after a successful run: lastJobName, durationSeconds, etc.
// ────────────────────────────────────────────────────────────────────────────

var _ = Describe("Status fields populate on success", Ordered, func() {
	const name = "status-fields-e2e"

	schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata: { name: %s, namespace: %s }
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  image: %s
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef: { name: postgres-credentials }
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: status-fields
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef: { name: aws-credentials }
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, localstackInCluster)

	BeforeAll(func() {
		applyManifest(schedule)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))

		// Trigger one successful run.
		job := name + "-manual"
		run("kubectl", "create", "job", job,
			"--from=cronjob/"+name, "-n", testNamespace)
		Eventually(func() string {
			complete, _ := runOutput("kubectl", "get", "job", job,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return complete
		}, 5*time.Minute, 3*time.Second).Should(Equal("True"))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("lastSuccessTime, lastJobName, lastDurationSeconds, totalRuns", func() {
		// Allow the reconciler a moment to observe the Job completion.
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "backupschedule", name,
				"-n", testNamespace,
				"-o", "jsonpath={.status.lastSuccessTime}")
			return out
		}, 2*time.Minute, 3*time.Second).ShouldNot(BeEmpty())

		// Check the rest of the enrichment in one shot.
		raw, err := runOutput("kubectl", "get", "backupschedule", name,
			"-n", testNamespace, "-o", "json")
		Expect(err).NotTo(HaveOccurred())

		var got struct {
			Status struct {
				LastJobName         string `json:"lastJobName"`
				LastDurationSeconds int    `json:"lastDurationSeconds"`
				TotalRuns           int    `json:"totalRuns"`
				ConsecutiveFailures int    `json:"consecutiveFailures"`
				ObservedGeneration  int    `json:"observedGeneration"`
			} `json:"status"`
		}
		Expect(json.Unmarshal([]byte(raw), &got)).To(Succeed())
		Expect(got.Status.LastJobName).NotTo(BeEmpty(), "lastJobName not populated")
		Expect(got.Status.LastDurationSeconds).To(BeNumerically(">=", 0),
			"lastDurationSeconds should be set after a successful run")
		Expect(got.Status.TotalRuns).To(BeNumerically(">=", 1),
			"totalRuns should be ≥1 after a successful run")
		Expect(got.Status.ConsecutiveFailures).To(Equal(0),
			"consecutiveFailures should be 0 after a successful run")
		Expect(got.Status.ObservedGeneration).To(BeNumerically(">", 0),
			"observedGeneration should be set")
	})

	It("Ready condition is True with reason=LastRunSucceeded", func() {
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "backupschedule", name,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].status}`)
			return out
		}).Should(Equal("True"))
		reason, _ := runOutput("kubectl", "get", "backupschedule", name,
			"-n", testNamespace,
			"-o", `jsonpath={.status.conditions[?(@.type=="Ready")].reason}`)
		Expect(reason).To(Equal("LastRunSucceeded"))
	})
})
