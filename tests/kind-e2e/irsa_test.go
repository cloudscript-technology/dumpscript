//go:build kind_e2e

package kinde2e_test

// irsa_test.go validates the full IRSA (IAM Roles for Service Accounts) flow:
//
//  BackupSchedule (serviceAccountName + roleARN, NO credentialsSecretRef)
//    → operator injects AWS_ROLE_ARN + AWS_WEB_IDENTITY_TOKEN_FILE + projected SA token volume
//    → dumpscript calls LocalStack STS:AssumeRoleWithWebIdentity with the SA token
//    → LocalStack returns temporary credentials
//    → dumpscript uses temporary credentials to upload the backup to LocalStack S3
//
// The OIDC provider, IAM role and ServiceAccount are created in BeforeSuite (infra_test.go).

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("IRSA (ServiceAccount-based auth)", Ordered, func() {
	const (
		name   = "irsa-e2e"
		prefix = "irsa-test"
		job    = "irsa-e2e-manual"
	)

	// BackupSchedule that uses the IRSA ServiceAccount instead of static AWS keys.
	// The operator will inject:
	//   - AWS_ROLE_ARN = irsaRoleARN
	//   - AWS_WEB_IDENTITY_TOKEN_FILE = /var/run/secrets/eks.amazonaws.com/serviceaccount/token
	//   - AWS_ENDPOINT_URL_STS = localstackInCluster  (because endpointURL is set)
	//   - projected ServiceAccount token volume at the token path above
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
  serviceAccountName: %s
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
      roleARN: "%s"
`, name, testNamespace, dumpscriptImg, irsaSAName, testNamespace,
		bucketName, prefix, localstackInCluster, irsaRoleARN)

	BeforeAll(func() {
		By("applying IRSA BackupSchedule (no credentialsSecretRef)")
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

	It("CronJob is created with projected SA token volume", func() {
		By("checking the projected volume is present in the CronJob job template")
		vols, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={.spec.jobTemplate.spec.template.spec.volumes[*].name}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(vols).To(ContainSubstring("aws-iam-token"),
			"expected projected token volume 'aws-iam-token' in CronJob, got: %s", vols)
	})

	It("CronJob container has AWS_ROLE_ARN and AWS_WEB_IDENTITY_TOKEN_FILE env vars", func() {
		envs, err := runOutput("kubectl", "get", "cronjob", name,
			"-n", testNamespace,
			"-o", `jsonpath={.spec.jobTemplate.spec.template.spec.containers[0].env[*].name}`)
		Expect(err).NotTo(HaveOccurred())
		Expect(envs).To(ContainSubstring("AWS_ROLE_ARN"))
		Expect(envs).To(ContainSubstring("AWS_WEB_IDENTITY_TOKEN_FILE"))
		Expect(envs).To(ContainSubstring("AWS_ENDPOINT_URL_STS"),
			"STS endpoint override must be set when endpointURL is configured")
	})

	It("backup job succeeds using ServiceAccount credentials (IRSA → LocalStack STS)", func() {
		By("triggering a backup job that authenticates via IRSA")
		run("kubectl", "create", "job", job,
			"--from=cronjob/"+name, "-n", testNamespace)

		By("waiting for Job to complete (credentials obtained via sts:AssumeRoleWithWebIdentity)")
		Eventually(func() (string, error) {
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
				logs, _ := runOutput("kubectl", "logs",
					"-l", "job-name="+job, "-n", testNamespace, "--tail=30")
				return "Failed", fmt.Errorf("IRSA job failed — pod logs:\n%s", logs)
			}
			return "", nil
		}, 5*time.Minute, 5*time.Second).Should(Equal("Complete"))
	})

	It("backup object is uploaded to S3 under the IRSA prefix", func() {
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
			"expected backup object under %s/daily/ in S3, got: %v", prefix, objects)

		GinkgoWriter.Printf("IRSA backup key: %s\n", found)
	})
})
