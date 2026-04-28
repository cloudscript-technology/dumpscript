//go:build kind_e2e

package kinde2e_test

// gcs_test.go validates the full GCS backend flow against fake-gcs-server:
//
//   BackupSchedule { storage.backend: gcs, gcs.endpoint: <fake-gcs URL> }
//     → operator injects GCS_BUCKET / GCS_PREFIX / GCS_ENDPOINT
//     → dumpscript uses cloud.google.com/go/storage with WithEndpoint +
//       WithoutAuthentication when GCS_ENDPOINT is set
//     → upload to fake-gcs-server bucket
//     → Restore CR downloads from the same emulator and applies to PostgreSQL

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GCS backend (fake-gcs-server)", Ordered, func() {
	const (
		name        = "gcs-e2e"
		prefix      = "gcs-test"
		job         = "gcs-e2e-manual"
		restoreName = "gcs-restore-e2e"
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
    backend: gcs
    gcs:
      bucket: %s
      prefix: "%s"
      projectID: test-project
      endpoint: %s
`, name, testNamespace, dumpscriptImg, testNamespace,
		gcsBucketName, prefix, fakeGCSInCluster)

	BeforeAll(func() {
		By("seeding the marker row in PostgreSQL (gcs_marker)")
		psql("CREATE TABLE IF NOT EXISTS gcs_marker (id SERIAL PRIMARY KEY, val TEXT);")
		psql("TRUNCATE gcs_marker;")
		psql("INSERT INTO gcs_marker (val) VALUES ('kind-gcs-marker');")

		By("applying GCS BackupSchedule")
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

	It("CronJob env contains GCS_BUCKET, GCS_PREFIX and GCS_ENDPOINT", func() {
		out, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={range .spec.jobTemplate.spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("GCS_BUCKET=" + gcsBucketName))
		Expect(out).To(ContainSubstring("GCS_PREFIX=" + prefix))
		Expect(out).To(ContainSubstring("GCS_ENDPOINT=" + fakeGCSInCluster))
	})

	It("backup job uploads to fake-gcs-server", func() {
		By("triggering a manual backup job")
		run("kubectl", "create", "job", job,
			"--from=cronjob/"+name, "-n", testNamespace)

		// Capture pod logs aggressively so we don't lose them when the job
		// fails and the pod gets garbage-collected.
		var capturedLogs string

		By("waiting for Job to complete")
		Eventually(func() (string, error) {
			// Save latest logs while the pod is still alive.
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
				return "Failed", fmt.Errorf("GCS backup job failed — captured pod logs:\n%s", capturedLogs)
			}
			return "", nil
		}, 5*time.Minute, 3*time.Second).Should(Equal("Complete"))
	})

	It("backup object exists in fake-gcs-server with the correct path structure", func() {
		var objects []string
		Eventually(func() ([]string, error) {
			return listGCSObjects(gcsBucketName)
		}).Should(Not(BeEmpty()))

		objects, _ = listGCSObjects(gcsBucketName)
		GinkgoWriter.Printf("GCS objects: %v\n", objects)

		for _, key := range objects {
			if strings.HasPrefix(key, prefix+"/daily/") && strings.HasSuffix(key, ".gz") {
				backupKey = key
				break
			}
		}
		Expect(backupKey).NotTo(BeEmpty(),
			"expected an object matching %s/daily/**/*.gz in fake-gcs, got: %v",
			prefix, objects)
	})

	It("Restore from GCS recovers the data", func() {
		Expect(backupKey).NotTo(BeEmpty(), "backupKey must be set by the preceding spec")

		By("dropping the marker table to simulate data loss")
		psql("DROP TABLE IF EXISTS gcs_marker;")

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
    backend: gcs
    gcs:
      bucket: %s
      projectID: test-project
      endpoint: %s
  notifications:
    stdout: true
`, restoreName, testNamespace, backupKey, dumpscriptImg, testNamespace,
			gcsBucketName, fakeGCSInCluster)

		By("applying Restore CR")
		applyManifest(restore)

		By("waiting for Restore phase=Succeeded")
		Eventually(func() string {
			phase, _ := runOutput("kubectl", "get", "restore", restoreName,
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			return phase
		}, 5*time.Minute, 5*time.Second).Should(Equal("Succeeded"))

		By("verifying the marker row was restored from GCS")
		out := psql("SELECT val FROM gcs_marker WHERE val = 'kind-gcs-marker';")
		Expect(out).To(ContainSubstring("kind-gcs-marker"))
	})
})
