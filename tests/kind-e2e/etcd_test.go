//go:build kind_e2e

package kinde2e_test

// etcd_test.go validates the etcd backup flow against the in-cluster etcd:v3.5
// server, using S3 (LocalStack) as the storage backend.
//
// Restore is intentionally not exercised: dumpscript's etcd restorer returns
// ErrEtcdRestoreUnsupported. The test covers backup only.

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("etcd backend (etcdctl snapshot save → S3)", Ordered, func() {
	const (
		name   = "etcd-e2e"
		prefix = "etcd-e2e"
		job    = "etcd-e2e-manual"
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
    type: etcd
    host: etcd.%s.svc.cluster.local
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
		By("seeding a marker key in etcd")
		etcdctl("put", "kind-etcd-marker", "ok")

		By("applying etcd BackupSchedule")
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

	It("CronJob env contains DB_TYPE=etcd and points at the in-cluster etcd Service", func() {
		out, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={range .spec.jobTemplate.spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("DB_TYPE=etcd"))
		Expect(out).To(ContainSubstring("DB_HOST=etcd." + testNamespace))
	})

	It("backup job uploads an etcd snapshot to S3", func() {
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
				return "Failed", fmt.Errorf("etcd backup job failed — captured pod logs:\n%s", capturedLogs)
			}
			return "", nil
		}, 5*time.Minute, 3*time.Second).Should(Equal("Complete"))
	})

	It("backup object exists in S3 with the correct path structure", func() {
		Eventually(func() ([]string, error) {
			return listS3Objects(bucketName)
		}).Should(Not(BeEmpty()))

		var found string
		objects, _ := listS3Objects(bucketName)
		for _, key := range objects {
			if strings.HasPrefix(key, prefix+"/daily/") && strings.HasSuffix(key, ".gz") {
				found = key
				break
			}
		}
		Expect(found).NotTo(BeEmpty(),
			"expected a key matching %s/daily/**/*.gz, got: %v", prefix, objects)
	})
})
