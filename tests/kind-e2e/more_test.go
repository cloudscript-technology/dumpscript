//go:build kind_e2e

package kinde2e_test

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// todayPath returns the date portion of an S3 key for today: YYYY/MM/DD
func todayPath() string {
	now := time.Now().UTC()
	return fmt.Sprintf("%04d/%02d/%02d", now.Year(), now.Month(), now.Day())
}

// ── Retention ─────────────────────────────────────────────────────────────────

var _ = Describe("Retention", Ordered, func() {
	const (
		name   = "retention-e2e"
		prefix = "retention-test"
	)

	oldKeys := []string{
		prefix + "/daily/2020/01/01/dump_20200101_000000.sql.gz",
		prefix + "/daily/2021/06/15/dump_20210615_120000.sql.gz",
		prefix + "/daily/2022/12/31/dump_20221231_235900.sql.gz",
	}

	schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: %s
  namespace: %s
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  retentionDays: 7
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
      prefix: "%s"
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, prefix, localstackInCluster)

	BeforeAll(func() {
		By("seeding old backup objects via aws-cli pod")
		for _, key := range oldKeys {
			seedS3Object(key)
		}

		By("confirming old objects are in S3")
		objects, _ := listS3Objects(bucketName)
		for _, key := range oldKeys {
			Expect(objects).To(ContainElement(key))
		}

		applyManifest(schedule)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))

		By("triggering backup with retentionDays=7")
		const jobName = "retention-e2e-manual"
		run("kubectl", "create", "job", jobName,
			"--from=cronjob/"+name, "-n", testNamespace)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return out
		}, 5*time.Minute, 5*time.Second).Should(Equal("True"))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("old backup objects are deleted by the retention sweep", func() {
		objects, err := listS3Objects(bucketName)
		Expect(err).NotTo(HaveOccurred())

		for _, key := range oldKeys {
			Expect(objects).NotTo(ContainElement(key),
				"old key %q should have been deleted by retentionDays=7", key)
		}
	})

	It("today's backup is preserved after retention sweep", func() {
		objects, err := listS3Objects(bucketName)
		Expect(err).NotTo(HaveOccurred())

		todayPrefix := prefix + "/daily/" + todayPath() + "/"
		found := false
		for _, key := range objects {
			if strings.HasPrefix(key, todayPrefix) && strings.HasSuffix(key, ".gz") {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(),
			"expected today's backup under %q in S3, got: %v", todayPrefix, objects)
	})
})

// ── Lock contention ───────────────────────────────────────────────────────────

var _ = Describe("Lock contention", Ordered, func() {
	const (
		name   = "lock-e2e"
		prefix = "lock-test"
	)

	// Lock key format: <prefix>/<periodicity>/<date>/.lock
	lockKey := prefix + "/daily/" + todayPath() + "/.lock"

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

	var objectsBefore []string

	BeforeAll(func() {
		By("pre-seeding today's lock to simulate a concurrent run")
		seedS3Object(lockKey)

		applyManifest(schedule)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))

		objectsBefore, _ = listS3Objects(bucketName)
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("job exits 0 (graceful skip) when lock is held", func() {
		const jobName = "lock-e2e-manual"
		run("kubectl", "create", "job", jobName,
			"--from=cronjob/"+name, "-n", testNamespace)

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return out
		}, 3*time.Minute, 5*time.Second).Should(Equal("True"),
			"job should exit 0 (skipped) — not fail — when lock is held")
	})

	It("no new backup object is uploaded when lock is held", func() {
		objectsAfter, err := listS3Objects(bucketName)
		Expect(err).NotTo(HaveOccurred())

		// The only new object should be the lock itself (already seeded); no dump.
		newObjects := []string{}
		for _, o := range objectsAfter {
			found := false
			for _, b := range objectsBefore {
				if o == b {
					found = true
					break
				}
			}
			if !found && strings.HasPrefix(o, prefix+"/") && strings.HasSuffix(o, ".gz") {
				newObjects = append(newObjects, o)
			}
		}
		Expect(newObjects).To(BeEmpty(),
			"no dump object should be uploaded when the lock is held, got: %v", newObjects)
	})
})

// ── Weekly periodicity ────────────────────────────────────────────────────────

var _ = Describe("Weekly periodicity", Ordered, func() {
	const (
		name   = "weekly-e2e"
		prefix = "weekly-test"
	)

	schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: %s
  namespace: %s
spec:
  schedule: "0 2 * * 0"
  periodicity: weekly
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

		const jobName = "weekly-e2e-manual"
		run("kubectl", "create", "job", jobName,
			"--from=cronjob/"+name, "-n", testNamespace)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return out
		}, 5*time.Minute, 5*time.Second).Should(Equal("True"))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("backup key path contains 'weekly/' segment", func() {
		objects, err := listS3Objects(bucketName)
		Expect(err).NotTo(HaveOccurred())

		var found string
		for _, key := range objects {
			if strings.HasPrefix(key, prefix+"/weekly/") && strings.HasSuffix(key, ".gz") {
				found = key
				break
			}
		}
		Expect(found).NotTo(BeEmpty(),
			"expected a key starting with %q/weekly/ in S3, got: %v", prefix, objects)
	})
})

// ── BackupSchedule starts suspended ──────────────────────────────────────────

var _ = Describe("BackupSchedule starts suspended", func() {
	It("CronJob is immediately suspended when BackupSchedule is created with suspend=true", func() {
		const name = "born-suspended"
		schedule := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: %s
  namespace: %s
spec:
  schedule: "0 2 * * *"
  periodicity: daily
  suspend: true
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
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, localstackInCluster)

		applyManifest(schedule)
		defer runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.spec.suspend}")
			return out
		}).Should(Equal("true"))

		// No Job should be running — jsonpath returns "" when list is empty.
		jobs, _ := runOutput("kubectl", "get", "jobs",
			"-l", "dumpscript.cloudscript.com.br/schedule="+name,
			"-n", testNamespace,
			"-o", "jsonpath={.items[*].metadata.name}")
		Expect(jobs).To(BeEmpty(),
			"no Jobs should be created for a suspended BackupSchedule")
	})
})

// ── Restore status fields ─────────────────────────────────────────────────────

var _ = Describe("Restore status fields", Ordered, func() {
	const (
		restoreName = "restore-status-check"
		schedName   = "restore-status-sched"
		schedPrefix = "restore-status"
		jobName     = "restore-status-backup"
	)
	var backupKeyForStatus string

	BeforeAll(func() {
		By("creating a dedicated BackupSchedule for this Describe block")
		applyManifest(fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: %s
  namespace: %s
spec:
  schedule: "0 2 * * *"
  periodicity: daily
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
      prefix: "%s"
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
`, schedName, testNamespace, dumpscriptImg, testNamespace, bucketName, schedPrefix, localstackInCluster))

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", schedName,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(schedName))

		By("running a backup job")
		run("kubectl", "create", "job", jobName, "--from=cronjob/"+schedName, "-n", testNamespace)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return out
		}, 5*time.Minute, 5*time.Second).Should(Equal("True"))

		runOutput("kubectl", "delete", "backupschedule", schedName, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")

		By("finding the backup key")
		Eventually(func() error {
			objects, err := listS3Objects(bucketName)
			if err != nil {
				return err
			}
			for _, key := range objects {
				if strings.HasPrefix(key, schedPrefix+"/daily/") {
					backupKeyForStatus = key
					return nil
				}
			}
			return fmt.Errorf("backup key not found yet in S3")
		}, 30*time.Second, 3*time.Second).Should(Succeed())

		By("applying Restore CR")
		applyManifest(fmt.Sprintf(`
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
`, restoreName, testNamespace, backupKeyForStatus, dumpscriptImg,
			testNamespace, bucketName, localstackInCluster))

		Eventually(func() string {
			phase, _ := runOutput("kubectl", "get", "restore", restoreName,
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			return phase
		}, 5*time.Minute, 5*time.Second).Should(Equal("Succeeded"))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "restore", restoreName, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("status.jobName is populated after Restore is created", func() {
		out := mustOutput("kubectl", "get", "restore", restoreName,
			"-n", testNamespace, "-o", "jsonpath={.status.jobName}")
		Expect(out).To(HavePrefix("restore-"),
			"jobName should start with 'restore-', got: %q", out)
	})

	It("status.startedAt is set", func() {
		out := mustOutput("kubectl", "get", "restore", restoreName,
			"-n", testNamespace, "-o", "jsonpath={.status.startedAt}")
		Expect(out).NotTo(BeEmpty(), "startedAt should be populated")
	})

	It("status.completedAt is set after Restore succeeds", func() {
		out := mustOutput("kubectl", "get", "restore", restoreName,
			"-n", testNamespace, "-o", "jsonpath={.status.completedAt}")
		Expect(out).NotTo(BeEmpty(), "completedAt should be populated on Succeeded phase")
	})
})

// ── lastFailureTime on failed job ─────────────────────────────────────────────

var _ = Describe("BackupSchedule failure tracking", Ordered, func() {
	const name = "failure-track-e2e"

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
  database:
    type: postgresql
    host: nonexistent-db.dumpscript-e2e.svc.cluster.local
    name: testdb
    credentialsSecretRef:
      name: postgres-credentials
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: "failure-test"
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
`, name, testNamespace, dumpscriptImg, bucketName, localstackInCluster)

	BeforeAll(func() {
		applyManifest(schedule)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))

		By("triggering a job that will fail (unreachable database)")
		const jobName = "failure-track-manual"
		run("kubectl", "create", "job", jobName,
			"--from=cronjob/"+name, "-n", testNamespace)

		By("waiting for the job to fail")
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Failed")].status}`)
			return out
		}, 3*time.Minute, 5*time.Second).Should(Equal("True"))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("status.lastFailureTime is set after a job fails", func() {
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "backupschedule", name,
				"-n", testNamespace,
				"-o", "jsonpath={.status.lastFailureTime}")
			return out
		}, 30*time.Second, 3*time.Second).ShouldNot(BeEmpty(),
			"lastFailureTime should be set when a job fails")
	})
})
