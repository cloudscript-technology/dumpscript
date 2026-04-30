//go:build kind_e2e

package kinde2e_test

// mongodb_test.go validates the full MongoDB backup/restore flow against the
// in-cluster mongo:7 server, using S3 (LocalStack) as the storage backend.
//
// MongoDB starts with auth enabled via MONGO_INITDB_ROOT_USERNAME/PASSWORD.
// `database.options: --authenticationDatabase=admin` is forwarded to both
// mongodump and mongorestore so the root user can authenticate.

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MongoDB backend (mongodump → S3 → mongorestore)", Ordered, func() {
	const (
		name        = "mongodb-e2e"
		prefix      = "mongodb-e2e"
		job         = "mongodb-e2e-manual"
		restoreName = "mongodb-restore-e2e"
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
    type: mongodb
    host: mongodb.%s.svc.cluster.local
    name: testdb
    options: "--authenticationDatabase=admin"
    credentialsSecretRef:
      name: mongodb-credentials
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
		By("seeding the marker document in MongoDB (mongo_marker)")
		mongoEval(`db.mongo_marker.deleteMany({}); db.mongo_marker.insertOne({val: "kind-mongo-marker"})`)

		By("applying MongoDB BackupSchedule")
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

	It("CronJob env contains DB_TYPE=mongodb, DB_NAME=testdb, and DUMP_OPTIONS with auth db", func() {
		out, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={range .spec.jobTemplate.spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("DB_TYPE=mongodb"))
		Expect(out).To(ContainSubstring("DB_HOST=mongodb." + testNamespace))
		Expect(out).To(ContainSubstring("DB_NAME=testdb"))
		Expect(out).To(ContainSubstring("--authenticationDatabase=admin"))
	})

	It("backup job uploads a mongodb archive to S3", func() {
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
				return "Failed", fmt.Errorf("MongoDB backup job failed — captured pod logs:\n%s", capturedLogs)
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

	It("Restore from S3 recovers the data into MongoDB", func() {
		Expect(backupKey).NotTo(BeEmpty(), "backupKey must be set by the preceding spec")

		By("dropping the marker collection to simulate data loss")
		mongoEval(`db.mongo_marker.drop()`)

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
    type: mongodb
    host: mongodb.%s.svc.cluster.local
    name: testdb
    options: "--authenticationDatabase=admin"
    credentialsSecretRef:
      name: mongodb-credentials
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

		By("verifying the marker document was restored")
		out := mongoEval(`db.mongo_marker.findOne({val: "kind-mongo-marker"}).val`)
		Expect(out).To(ContainSubstring("kind-mongo-marker"))
	})
})
