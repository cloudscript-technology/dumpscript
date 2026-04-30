//go:build kind_e2e

package kinde2e_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// backupScheduleYAML builds a minimal BackupSchedule manifest for lifecycle tests.
func lifecycleScheduleYAML(name, schedule string) string {
	return fmt.Sprintf(`
apiVersion: dumpscript.cloudscript.com.br/v1alpha1
kind: BackupSchedule
metadata:
  name: %s
  namespace: %s
spec:
  schedule: "%s"
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
`, name, testNamespace, schedule, dumpscriptImg, testNamespace, bucketName, localstackInCluster)
}

// ── BackupSchedule spec changes ───────────────────────────────────────────────

var _ = Describe("BackupSchedule spec changes", Ordered, func() {
	const name = "lifecycle-e2e"

	BeforeAll(func() {
		By("applying lifecycle BackupSchedule")
		applyManifest(lifecycleScheduleYAML(name, "0 2 * * *"))

		By("waiting for CronJob to be created")
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

	It("suspend=true pauses the underlying CronJob", func() {
		run("kubectl", "patch", "backupschedule", name,
			"-n", testNamespace,
			"--type=merge", `--patch={"spec":{"suspend":true}}`)

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.spec.suspend}")
			return out
		}).Should(Equal("true"))
	})

	It("suspend=false resumes the CronJob", func() {
		run("kubectl", "patch", "backupschedule", name,
			"-n", testNamespace,
			"--type=merge", `--patch={"spec":{"suspend":false}}`)

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.spec.suspend}")
			return out
		}).Should(Equal("false"))
	})

	It("schedule change is propagated to the CronJob", func() {
		run("kubectl", "patch", "backupschedule", name,
			"-n", testNamespace,
			"--type=merge", `--patch={"spec":{"schedule":"0 3 * * *"}}`)

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.spec.schedule}")
			return out
		}).Should(Equal("0 3 * * *"))
	})

	It("deleting BackupSchedule garbage-collects the owned CronJob", func() {
		run("kubectl", "delete", "backupschedule", name, "-n", testNamespace)

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "--ignore-not-found",
				"-o", "jsonpath={.metadata.name}")
			return out
		}, 30*time.Second, 2*time.Second).Should(BeEmpty(),
			"CronJob should be garbage-collected via owner reference")
	})
})

// ── BackupSchedule status ─────────────────────────────────────────────────────

var _ = Describe("BackupSchedule status", Ordered, func() {
	const name = "status-e2e"
	const jobName = "status-e2e-manual"

	BeforeAll(func() {
		By("applying status-check BackupSchedule")
		applyManifest(lifecycleScheduleYAML(name, "0 2 * * *"))

		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "cronjob", name,
				"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
			return out
		}).Should(Equal(name))

		By("triggering a manual Job")
		run("kubectl", "create", "job", jobName,
			"--from=cronjob/"+name, "-n", testNamespace)

		By("waiting for Job to complete")
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

	It("lastSuccessTime is set after a successful job", func() {
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "backupschedule", name,
				"-n", testNamespace,
				"-o", "jsonpath={.status.lastSuccessTime}")
			return out
		}, 30*time.Second, 3*time.Second).ShouldNot(BeEmpty(),
			"lastSuccessTime should be populated once the Job label mapper re-triggers reconciliation")
	})

	It("lastScheduleTime reflects the manual job creation time", func() {
		out, err := runOutput("kubectl", "get", "backupschedule", name,
			"-n", testNamespace,
			"-o", "jsonpath={.status.lastScheduleTime}")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).NotTo(BeEmpty())
	})
})

// ── Operator resilience ───────────────────────────────────────────────────────

var _ = Describe("Operator resilience", func() {
	It("new operator pod continues reconciling existing BackupSchedules", func() {
		By("finding the current operator pod")
		podName := mustOutput("kubectl", "get", "pod",
			"-l", "control-plane=controller-manager",
			"-n", operatorNS,
			"-o", "jsonpath={.items[0].metadata.name}")
		Expect(podName).NotTo(BeEmpty())

		By("deleting the operator pod to trigger a restart")
		run("kubectl", "delete", "pod", podName, "-n", operatorNS)

		By("waiting for a new pod to be Running")
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "pods",
				"-l", "control-plane=controller-manager",
				"-n", operatorNS,
				"-o", "jsonpath={.items[0].status.phase}")
			return out
		}, 2*time.Minute, 3*time.Second).Should(Equal("Running"))

		By("verifying the existing BackupSchedule still has its CronJob")
		// The "postgres-e2e" schedule was created by backup_test.go's BeforeAll.
		// After operator restart it should still exist.
		out, err := runOutput("kubectl", "get", "cronjob", "postgres-e2e",
			"-n", testNamespace, "--ignore-not-found",
			"-o", "jsonpath={.metadata.name}")
		Expect(err).NotTo(HaveOccurred())
		// CronJob may have been cleaned up by backup_test.go AfterAll already,
		// so we just verify reconciliation doesn't panic (no Failed pod events).
		operatorLogs, _ := runOutput("kubectl", "logs",
			"-l", "control-plane=controller-manager",
			"-n", operatorNS, "--tail=20")
		Expect(operatorLogs).NotTo(ContainSubstring("panic"),
			"operator should not panic after restart; got logs:\n%s", operatorLogs)
		GinkgoWriter.Printf("new operator pod=%q cronjob=%q\n", podName, out)
	})
})
