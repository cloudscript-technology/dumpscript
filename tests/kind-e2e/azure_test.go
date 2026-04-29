//go:build kind_e2e

package kinde2e_test

// azure_test.go validates the full Azure Blob backend flow against Azurite:
//
//   BackupSchedule { storage.backend: azure, azure.endpoint: <azurite URL> }
//     → operator injects AZURE_STORAGE_ACCOUNT/CONTAINER/PREFIX/ENDPOINT
//       + AZURE_STORAGE_KEY (from Secret) into the dumpscript pod
//     → dumpscript uploads to Azurite via the azblob SDK using SharedKey auth
//     → Restore CR downloads from Azurite and applies to PostgreSQL

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Azure Blob backend (Azurite)", Ordered, func() {
	const (
		name        = "azure-e2e"
		prefix      = "azure-test"
		job         = "azure-e2e-manual"
		restoreName = "azure-restore-e2e"
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
  extraEnv:
    - name: AZURE_STORAGE_CREATE_CONTAINER_IF_MISSING
      value: "true"
  database:
    type: postgresql
    host: postgres.%s.svc.cluster.local
    name: testdb
    credentialsSecretRef:
      name: postgres-credentials
  storage:
    backend: azure
    azure:
      account: %s
      container: %s
      prefix: "%s"
      endpoint: %s
      credentialsSecretRef:
        name: azure-credentials
        sharedKeyKey: AZURE_STORAGE_KEY
`, name, testNamespace, dumpscriptImg, testNamespace,
		azureAccount, azureContainer, prefix, azuriteInCluster)

	BeforeAll(func() {
		By("seeding the marker row in PostgreSQL (azure_marker)")
		psql("CREATE TABLE IF NOT EXISTS azure_marker (id SERIAL PRIMARY KEY, val TEXT);")
		psql("TRUNCATE azure_marker;")
		psql("INSERT INTO azure_marker (val) VALUES ('kind-azure-marker');")

		By("applying Azure BackupSchedule")
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

	It("CronJob env contains AZURE_STORAGE_* vars and key from Secret", func() {
		// Names + plain values
		out, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={range .spec.jobTemplate.spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring("AZURE_STORAGE_ACCOUNT=" + azureAccount))
		Expect(out).To(ContainSubstring("AZURE_STORAGE_CONTAINER=" + azureContainer))
		Expect(out).To(ContainSubstring("AZURE_STORAGE_PREFIX=" + prefix))
		Expect(out).To(ContainSubstring("AZURE_STORAGE_ENDPOINT=" + azuriteInCluster))

		// Names alone (AZURE_STORAGE_KEY is sourced from a Secret, no plain value)
		names, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={.spec.jobTemplate.spec.template.spec.containers[0].env[*].name}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).To(ContainSubstring("AZURE_STORAGE_KEY"))
	})

	It("backup job uploads to Azurite", func() {
		By("triggering a manual standalone backup pod (no Job controller)")
		// Spawn a Pod directly with the CronJob's pod-template spec, but
		// restartPolicy=Never and no controller — Job controller otherwise
		// deletes the pod on BackoffLimit=0 failure before we can read logs.
		podName := job + "-pod"
		podSpec := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  restartPolicy: Never
  containers:
    - name: dumpscript
      image: %s
      imagePullPolicy: IfNotPresent
      args: ["dump"]
      env:
        - { name: DB_TYPE,        value: postgresql }
        - { name: DB_HOST,        value: postgres.%s.svc.cluster.local }
        - { name: DB_NAME,        value: testdb }
        - { name: DB_PORT,        value: "5432" }
        - { name: PERIODICITY,    value: daily }
        - { name: STORAGE_BACKEND,         value: azure }
        - { name: AZURE_STORAGE_ACCOUNT,   value: %s }
        - { name: AZURE_STORAGE_CONTAINER, value: %s }
        - { name: AZURE_STORAGE_PREFIX,    value: %s }
        - { name: AZURE_STORAGE_ENDPOINT,  value: %s }
        - { name: AZURE_STORAGE_CREATE_CONTAINER_IF_MISSING, value: "true" }
        - { name: NOTIFY_STDOUT,  value: "true" }
        - name: DB_USER
          valueFrom: { secretKeyRef: { name: postgres-credentials, key: username } }
        - name: DB_PASSWORD
          valueFrom: { secretKeyRef: { name: postgres-credentials, key: password } }
        - name: AZURE_STORAGE_KEY
          valueFrom: { secretKeyRef: { name: azure-credentials, key: AZURE_STORAGE_KEY } }
`, podName, testNamespace, dumpscriptImg, testNamespace,
			azureAccount, azureContainer, prefix, azuriteInCluster)
		applyManifest(podSpec)
		defer runOutput("kubectl", "delete", "pod", podName, "-n", testNamespace, "--ignore-not-found") //nolint:errcheck

		By("waiting for Pod to reach Succeeded or Failed phase")
		var phase string
		Eventually(func() string {
			out, _ := runOutput("kubectl", "get", "pod", podName,
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			phase = out
			return out
		}, 3*time.Minute, 1*time.Second).Should(Or(Equal("Succeeded"), Equal("Failed")))

		logs, _ := runOutput("kubectl", "logs", podName, "-n", testNamespace, "--tail=-1")
		GinkgoWriter.Printf("--- DUMPSCRIPT POD LOGS ---\n%s\n--- END LOGS ---\n", logs)
		Expect(phase).To(Equal("Succeeded"),
			"dumpscript pod failed.\nLOGS:\n%s", logs)

		// Extract the uploaded backup key from the dumpscript success event.
		// `key` fields are redacted by the slog handler; the unredacted location
		// appears in `path` like:
		//   {"event":"success","path":"azure://dumpscript-azure-e2e/azure-test/daily/2026/04/29/dump_….sql.gz"}
		// Strip the "azure://<container>/" prefix to recover just the blob key.
		azureURIPrefix := "azure://" + azureContainer + "/"
		for _, ln := range strings.Split(logs, "\n") {
			if i := strings.Index(ln, `"path":"`+azureURIPrefix); i >= 0 {
				rest := ln[i+len(`"path":"`)+len(azureURIPrefix):]
				if j := strings.Index(rest, `"`); j >= 0 {
					candidate := rest[:j]
					if strings.HasPrefix(candidate, prefix+"/daily/") && strings.HasSuffix(candidate, ".gz") {
						backupKey = candidate
						break
					}
				}
			}
		}
		Expect(backupKey).NotTo(BeEmpty(),
			"could not extract uploaded backup key from dumpscript logs:\n%s", logs)
	})

	It("Restore from Azure recovers the data", func() {
		Expect(backupKey).NotTo(BeEmpty(), "backupKey must be set by the preceding spec")

		By("dropping the marker table to simulate data loss")
		psql("DROP TABLE IF EXISTS azure_marker;")

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
    backend: azure
    azure:
      account: %s
      container: %s
      endpoint: %s
      credentialsSecretRef:
        name: azure-credentials
        sharedKeyKey: AZURE_STORAGE_KEY
  notifications:
    stdout: true
`, restoreName, testNamespace, backupKey, dumpscriptImg, testNamespace,
			azureAccount, azureContainer, azuriteInCluster)

		By("applying Restore CR")
		applyManifest(restore)

		By("waiting for Restore phase=Succeeded")
		Eventually(func() string {
			phase, _ := runOutput("kubectl", "get", "restore", restoreName,
				"-n", testNamespace, "-o", "jsonpath={.status.phase}")
			return phase
		}, 5*time.Minute, 5*time.Second).Should(Equal("Succeeded"))

		By("verifying the marker row was restored from Azurite")
		out := psql("SELECT val FROM azure_marker WHERE val = 'kind-azure-marker';")
		Expect(out).To(ContainSubstring("kind-azure-marker"))
	})
})
