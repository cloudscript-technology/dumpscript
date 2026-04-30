//go:build kind_e2e

package kinde2e_test

// mysql_test.go validates the full MySQL backup/restore flow against the in-cluster
// mysql:8 server, using S3 (LocalStack) as the storage backend.

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MySQL backend (mysqldump → S3 → mysql restore)", Ordered, func() {
	const (
		name        = "mysql-e2e"
		prefix      = "mysql-e2e"
		job         = "mysql-e2e-manual"
		restoreName = "mysql-restore-e2e"
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
    type: mysql
    host: mysql.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef:
      name: mysql-credentials
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
		By("seeding the marker row in MySQL (mysql_marker)")
		mysqlExec("CREATE TABLE IF NOT EXISTS mysql_marker (id INT AUTO_INCREMENT PRIMARY KEY, val VARCHAR(64));")
		mysqlExec("TRUNCATE TABLE mysql_marker;")
		mysqlExec("INSERT INTO mysql_marker (val) VALUES ('kind-mysql-marker');")

		By("applying MySQL BackupSchedule")
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

	It("CronJob env contains DB_TYPE=mysql and points at the in-cluster mysql Service", func() {
		out, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={range .spec.jobTemplate.spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("DB_TYPE=mysql"))
		Expect(out).To(ContainSubstring("DB_HOST=mysql." + testNamespace))
		Expect(out).To(ContainSubstring("DB_NAME=testdb"))
	})

	It("backup job uploads a mysql dump to S3", func() {
		By("triggering a manual backup job")
		run("kubectl", "create", "job", job,
			"--from=cronjob/"+name, "-n", testNamespace)

		var capturedLogs string
		Eventually(func() (string, error) {
			if logs, err := runOutput("kubectl", "logs",
				"-l", "job-name="+job, "-n", testNamespace, "--tail=50"); err == nil && logs != "" {
				capturedLogs = logs
			}
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
				return "Failed", fmt.Errorf("MySQL backup job failed — captured pod logs:\n%s", capturedLogs)
			}
			return "", nil
		}, 5*time.Minute, 3*time.Second).Should(Equal("Complete"))
	})

	It("backup object exists in S3 with the correct path structure", func() {
		Eventually(func() ([]string, error) {
			return listS3Objects(bucketName)
		}).Should(Not(BeEmpty()))

		objects, _ := listS3Objects(bucketName)
		for _, key := range objects {
			if strings.HasPrefix(key, prefix+"/daily/") && strings.HasSuffix(key, ".gz") {
				backupKey = key
				break
			}
		}
		Expect(backupKey).NotTo(BeEmpty(),
			"expected a key matching %s/daily/**/*.gz, got: %v", prefix, objects)
	})

	It("Restore from S3 recovers the data into MySQL", func() {
		Expect(backupKey).NotTo(BeEmpty(), "backupKey must be set by the preceding spec")

		By("dropping the marker table to simulate data loss")
		mysqlExec("DROP TABLE IF EXISTS mysql_marker;")

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
    type: mysql
    host: mysql.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef:
      name: mysql-credentials
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

		By("waiting for Restore phase=Succeeded")
		Eventually(func() string {
			phase, _ := runOutput("kubectl", "get", "restore", restoreName,
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			return phase
		}, 5*time.Minute, 5*time.Second).Should(Equal("Succeeded"))

		By("verifying the marker row was restored from S3")
		out := mysqlExec("SELECT val FROM mysql_marker WHERE val = 'kind-mysql-marker';")
		Expect(out).To(ContainSubstring("kind-mysql-marker"))
	})
})
