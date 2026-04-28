//go:build kind_e2e

package kinde2e_test

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// localstackInCluster is the LocalStack endpoint reachable from pods inside kind.
const localstackInCluster = "http://localstack." + testNamespace + ".svc.cluster.local:4566"

var _ = Describe("BackupSchedule → S3 → Restore", Ordered, func() {
	const (
		scheduleName = "postgres-e2e"
		restoreName  = "postgres-restore-e2e"
	)

	// backupKey is set after the backup job succeeds and used by the Restore test.
	var backupKey string

	backupSchedule := fmt.Sprintf(`
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
      region: us-east-1
      endpointURL: %s
      credentialsSecretRef:
        name: aws-credentials
`, scheduleName, testNamespace, dumpscriptImg, testNamespace, bucketName, localstackInCluster)

	BeforeAll(func() {
		By("seeding PostgreSQL with a marker row")
		psql("CREATE TABLE IF NOT EXISTS e2e_marker (id SERIAL PRIMARY KEY, val TEXT);")
		psql("TRUNCATE e2e_marker;")
		psql("INSERT INTO e2e_marker (val) VALUES ('kind-e2e-marker');")
	})

	It("operator reconciles BackupSchedule → CronJob", func() {
		By("applying BackupSchedule CR")
		applyManifest(backupSchedule)

		By("waiting for the CronJob to be created")
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", scheduleName,
				"-n", testNamespace,
				"-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(scheduleName))
	})

	It("manual Job trigger completes successfully", func() {
		jobName := fmt.Sprintf("postgres-e2e-manual-%d", time.Now().Unix())

		By("creating a Job from the CronJob")
		run("kubectl", "create", "job", jobName,
			"--from=cronjob/"+scheduleName,
			"-n", testNamespace)

		By("waiting for Job to complete (up to 5 min)")
		Eventually(func() (string, error) {
			complete, err := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
			if err != nil {
				return "", err
			}
			if complete == "True" {
				return "Complete", nil
			}
			// Surface failure early instead of waiting for timeout.
			failed, _ := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace,
				"-o", `jsonpath={.status.conditions[?(@.type=="Failed")].status}`)
			if failed == "True" {
				logs, _ := runOutput("kubectl", "logs",
					"-l", "job-name="+jobName,
					"-n", testNamespace, "--tail=50")
				return "Failed", fmt.Errorf("job failed — pod logs:\n%s", logs)
			}
			return "", nil
		}, 5*time.Minute, 5*time.Second).Should(Equal("Complete"))
	})

	It("backup object is present in S3 with the correct path structure", func() {
		By("listing objects in the bucket via LocalStack")
		var objects []string
		Eventually(func() ([]string, error) {
			return listS3Objects(bucketName)
		}).Should(Not(BeEmpty()))

		objects, _ = listS3Objects(bucketName)
		GinkgoWriter.Printf("S3 objects: %v\n", objects)

		By("verifying path structure: <periodicity>/YYYY/MM/DD/dump_*.sql.gz")
		for _, key := range objects {
			// S3_PREFIX is not set so the key starts directly with the periodicity.
			if strings.HasPrefix(key, "daily/") && strings.HasSuffix(key, ".gz") {
				backupKey = key
				break
			}
		}
		Expect(backupKey).NotTo(BeEmpty(),
			"expected an object matching daily/**/*.gz, got: %v", objects)
	})

	It("operator reconciles Restore → Job and data is recovered", func() {
		Expect(backupKey).NotTo(BeEmpty(), "backupKey must be set by the preceding backup test")

		By("dropping the marker table to simulate data loss")
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
`, restoreName, testNamespace, backupKey, dumpscriptImg, testNamespace, bucketName, localstackInCluster)

		By("applying Restore CR")
		applyManifest(restore)

		By("waiting for Restore phase = Succeeded (up to 5 min)")
		Eventually(func(g Gomega) {
			phase, _ := runOutput("kubectl", "get", "restore", restoreName,
				"-n", testNamespace,
				"-o", "jsonpath={.status.phase}")

			// Print diagnostics on every poll so we can see what's happening.
			jobName := "restore-" + restoreName
			jobStatus, _ := runOutput("kubectl", "get", "job", jobName,
				"-n", testNamespace, "--ignore-not-found",
				"-o", "jsonpath=active={.status.active} succeeded={.status.succeeded} failed={.status.failed}")
			podLogs, _ := runOutput("kubectl", "logs",
				"-l", "dumpscript.cloudscript.com.br/restore="+restoreName,
				"-n", testNamespace, "--tail=20", "--ignore-errors")
			GinkgoWriter.Printf("restore phase=%q job=%q\n%s\n", phase, jobStatus, podLogs)

			if phase == "Failed" {
				msg, _ := runOutput("kubectl", "get", "restore", restoreName,
					"-n", testNamespace,
					"-o", "jsonpath={.status.message}")
				g.Expect(phase).NotTo(Equal("Failed"), "restore failed: %s", msg)
			}
			g.Expect(phase).To(Equal("Succeeded"))
		}, 5*time.Minute, 5*time.Second).Should(Succeed())

		By("verifying the marker row was restored")
		out := psql("SELECT val FROM e2e_marker WHERE val = 'kind-e2e-marker';")
		Expect(out).To(ContainSubstring("kind-e2e-marker"))
	})

	AfterAll(func() {
		By("cleaning up CRs")
		runOutput("kubectl", "delete", "backupschedule", scheduleName, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
		runOutput("kubectl", "delete", "restore", restoreName, //nolint:errcheck
			"-n", testNamespace, "--ignore-not-found")
	})
})
