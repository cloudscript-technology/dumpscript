//go:build kind_e2e

package kinde2e_test

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// psqlDB runs a SQL statement against an arbitrary database on the postgres pod.
func psqlDB(database, sql string) string {
	GinkgoHelper()
	return kubectlExec(testNamespace, pgPodName(), "postgres",
		"psql", "-U", "testuser", "-d", database, "-t", "-c", sql)
}

// ── S3 prefix ─────────────────────────────────────────────────────────────────

var _ = Describe("S3 prefix", Ordered, func() {
	const (
		name   = "prefix-e2e"
		prefix = "myapp/backups"
		job    = "prefix-e2e-manual"
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

		run("kubectl", "create", "job", job, "--from=cronjob/"+name, "-n", testNamespace)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "job", job,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return out
		}, 5*time.Minute, 5*time.Second).Should(Equal("True"))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("backup object key starts with the configured S3 prefix", func() {
		objects, err := listS3Objects(bucketName)
		Expect(err).NotTo(HaveOccurred())

		var found string
		for _, key := range objects {
			if strings.HasPrefix(key, prefix+"/daily/") && strings.HasSuffix(key, ".gz") {
				found = key
				break
			}
		}
		Expect(found).NotTo(BeEmpty(),
			"expected a key starting with %q/daily/ in S3, got: %v", prefix, objects)
	})
})

// ── Stdout notification ───────────────────────────────────────────────────────

var _ = Describe("Stdout notification", Ordered, func() {
	const (
		name = "notify-e2e"
		job  = "notify-e2e-manual"
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
    notifySuccess: true
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

	BeforeAll(func() {
		applyManifest(schedule)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))

		run("kubectl", "create", "job", job, "--from=cronjob/"+name, "-n", testNamespace)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "job", job,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return out
		}, 5*time.Minute, 5*time.Second).Should(Equal("True"))
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "backupschedule", name, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	It("pod logs contain a structured notification JSON on success", func() {
		logs, err := runOutput("kubectl", "logs",
			"-l", "job-name="+job,
			"-n", testNamespace)
		Expect(err).NotTo(HaveOccurred())
		// The stdout notifier emits {"event":"success",...} (not a slog line).
		Expect(logs).To(ContainSubstring(`"event":"success"`),
			"expected a stdout notification JSON line in pod logs:\n%s", logs)
	})
})

// ── CronJob history limits ────────────────────────────────────────────────────

var _ = Describe("CronJob history limits", Ordered, func() {
	const name = "history-e2e"

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
  failedJobsHistoryLimit: 2
  successfulJobsHistoryLimit: 1
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

	It("CronJob reflects custom failedJobsHistoryLimit", func() {
		out := mustOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", "jsonpath={.spec.failedJobsHistoryLimit}")
		Expect(out).To(Equal("2"))
	})

	It("CronJob reflects custom successfulJobsHistoryLimit", func() {
		out := mustOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", "jsonpath={.spec.successfulJobsHistoryLimit}")
		Expect(out).To(Equal("1"))
	})
})

// ── Multiple BackupSchedules ──────────────────────────────────────────────────

var _ = Describe("Multiple BackupSchedules", Ordered, func() {
	const (
		nameA = "multi-a"
		nameB = "multi-b"
	)

	makeSchedule := func(name string) string {
		return fmt.Sprintf(`
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
`, name, testNamespace, dumpscriptImg, testNamespace, bucketName, name, localstackInCluster)
	}

	BeforeAll(func() {
		applyManifest(makeSchedule(nameA))
		applyManifest(makeSchedule(nameB))
		for _, n := range []string{nameA, nameB} {
			n := n
			Eventually(func() string {
				out, _ := runOutput("kubectl", "get", "cronjob", n,
					"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
				return out
			}).Should(Equal(n))
		}
	})

	AfterAll(func() {
		for _, n := range []string{nameA, nameB} {
			runOutput("kubectl", "delete", "backupschedule", n, //nolint:errcheck
				"-n", testNamespace, "--ignore-not-found")
		}
	})

	It("both BackupSchedules have independent CronJobs", func() {
		for _, n := range []string{nameA, nameB} {
			out := mustOutput("kubectl", "get", "cronjob", n,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			Expect(out).To(Equal(n))
		}
	})

	It("deleting one BackupSchedule does not affect the other", func() {
		run("kubectl", "delete", "backupschedule", nameA, "-n", testNamespace)

		By("CronJob A is gone")
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", nameA,
				"-n", testNamespace, "--ignore-not-found",
				"-o", "jsonpath={.metadata.name}")
			return out
		}, 30*time.Second, 2*time.Second).Should(BeEmpty())

		By("CronJob B still exists")
		out := mustOutput("kubectl", "get", "cronjob", nameB,
			"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
		Expect(out).To(Equal(nameB))
	})
})

// ── Restore advanced ──────────────────────────────────────────────────────────

var _ = Describe("Restore advanced", Ordered, func() {
	const createDBDatabase = "createdb_test"
	var createDBBackupKey string

	BeforeAll(func() {
		By("creating a dedicated database for createDB tests")
		psqlDB("postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", createDBDatabase))
		psqlDB("postgres", fmt.Sprintf("CREATE DATABASE %s;", createDBDatabase))
		psqlDB(createDBDatabase, "CREATE TABLE cargo (id SERIAL PRIMARY KEY, val TEXT);")
		psqlDB(createDBDatabase, "INSERT INTO cargo (val) VALUES ('createdb-payload');")

		By("backing up createdb_test")
		const schedName = "createdb-sched"
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
    name: %s
    credentialsSecretRef:
      name: postgres-credentials
  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: "createdb-test"
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
`, schedName, testNamespace, dumpscriptImg, testNamespace, createDBDatabase, bucketName, localstackInCluster))

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", schedName,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(schedName))

		const jobName = "createdb-backup-job"
		run("kubectl", "create", "job", jobName, "--from=cronjob/"+schedName, "-n", testNamespace)
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			return out
		}, 5*time.Minute, 5*time.Second).Should(Equal("True"))

		// Find the backup key.
		Eventually(func() error {
			objects, err := listS3Objects(bucketName)
			if err != nil {
				return err
			}
			for _, key := range objects {
				if strings.HasPrefix(key, "createdb-test/daily/") {
					createDBBackupKey = key
					return nil
				}
			}
			return fmt.Errorf("backup key not found yet")
		}, 30*time.Second, 3*time.Second).Should(Succeed())

		runOutput("kubectl", "delete", "backupschedule", schedName, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})

	AfterAll(func() {
		runOutput("kubectl", "delete", "restore", "restore-createdb", //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
		psqlDB("postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", createDBDatabase))
	})

	It("Restore with createDB=true recreates the database and recovers data", func() {
		By("dropping the database entirely")
		// Terminate connections first (PG13+)
		psqlDB("postgres", fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='%s';",
			createDBDatabase))
		psqlDB("postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", createDBDatabase))

		restore := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: Restore
metadata:
  name: restore-createdb
  namespace: %s
spec:
  sourceKey: "%s"
  createDB: true
  image: %s
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: %s
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
`, testNamespace, createDBBackupKey, dumpscriptImg, testNamespace, createDBDatabase, bucketName, localstackInCluster)

		By("applying Restore CR with createDB=true")
		applyManifest(restore)

		By("waiting for Restore phase=Succeeded")
		Eventually(func() string {
			phase, _ := runOutput("kubectl", "get", "restore", "restore-createdb",
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			return phase
		}, 5*time.Minute, 5*time.Second).Should(Equal("Succeeded"))

		By("verifying the database exists and data is intact")
		out := psqlDB(createDBDatabase,
			"SELECT val FROM cargo WHERE val = 'createdb-payload';")
		Expect(out).To(ContainSubstring("createdb-payload"))
	})

	It("Restore with ttlSecondsAfterFinished cleans up the Job automatically", func() {
		const ttl = 15

		objects, err := listS3Objects(bucketName)
		Expect(err).NotTo(HaveOccurred())

		var key string
		for _, k := range objects {
			if strings.HasSuffix(k, ".gz") {
				key = k
				break
			}
		}
		Expect(key).NotTo(BeEmpty())

		restore := fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: Restore
metadata:
  name: restore-ttl
  namespace: %s
spec:
  sourceKey: "%s"
  ttlSecondsAfterFinished: %d
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
`, testNamespace, key, ttl, dumpscriptImg, testNamespace, bucketName, localstackInCluster)

		By("applying Restore CR with ttlSecondsAfterFinished=15")
		applyManifest(restore)
		defer runOutput("kubectl", "delete", "restore", "restore-ttl", //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")

		By("waiting for Restore phase=Succeeded")
		Eventually(func() string {
			phase, _ := runOutput("kubectl", "get", "restore", "restore-ttl",
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			return phase
		}, 5*time.Minute, 5*time.Second).Should(Equal("Succeeded"))

		jobName := mustOutput("kubectl", "get", "restore", "restore-ttl",
			"-n", testNamespace, "-o", "jsonpath={.status.jobName}")

		By(fmt.Sprintf("verifying Job %q is deleted within %ds TTL", jobName, ttl+10))
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace, "--ignore-not-found",
				"-o", "jsonpath={.metadata.name}")
			return out
		}, time.Duration(ttl+30)*time.Second, 3*time.Second).Should(BeEmpty(),
			"Job should be garbage-collected by Kubernetes TTL controller after %ds", ttl)
	})
})
