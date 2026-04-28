//go:build kind_e2e

package kinde2e_test

// features_test.go covers the new dumpscript binary features that landed in
// the operator-hardening PR — the ones that aren't covered by the per-engine
// backup/restore specs:
//
//   1. DRY_RUN=true skips dump + upload (Job completes, S3 stays empty for
//      the dry-run prefix).
//   2. COMPRESSION_TYPE=zstd produces .zst keys in S3.
//   3. Uploaded objects carry the expected tags (managed_by=dumpscript,
//      engine=postgresql, periodicity=daily) — verified via aws CLI.

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Dumpscript binary features", Ordered, func() {

	// -----------------------------------------------------------------
	// Dry-run mode
	// -----------------------------------------------------------------
	Describe("DRY_RUN=true", Ordered, func() {
		const (
			scheduleName = "dryrun-e2e"
			job          = "dryrun-e2e-manual"
			prefix       = "dryrun-e2e"
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
  dryRun: true
`, scheduleName, testNamespace, dumpscriptImg, testNamespace, bucketName, prefix, localstackInCluster)

		BeforeAll(func() {
			By("applying BackupSchedule with DRY_RUN env")
			applyManifest(schedule)
			Eventually(func() string {
				out, _ := runOutput("kubectl", "get", "cronjob", scheduleName,
					"-n", testNamespace, "-o", "jsonpath={.metadata.name}")
				return out
			}).Should(Equal(scheduleName))
		})

		AfterAll(func() {
			runOutput("kubectl", "delete", "backupschedule", scheduleName, //nolint:errcheck
				"-n", testNamespace, "--ignore-not-found")
		})

		It("CronJob env contains DRY_RUN=true (spec.dryRun plumbed through)", func() {
			out, err := runOutput("kubectl", "get", "cronjob", scheduleName,
				"-n", testNamespace,
				"-o", `jsonpath={range .spec.jobTemplate.spec.template.spec.containers[0].env[*]}{.name}={.value}{"\n"}{end}`)
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring("DRY_RUN=true"))
		})

		It("Job completes successfully but uploads no .gz/.zst object", func() {
			By("triggering manual Job")
			run("kubectl", "create", "job", job,
				"--from=cronjob/"+scheduleName, "-n", testNamespace)

			By("waiting for Job to complete")
			Eventually(func() string {
				complete, _ := runOutput("kubectl", "get", "job", job,
					"-n", testNamespace,
					"-o", `jsonpath={.status.conditions[?(@.type=="Complete")].status}`)
				return complete
			}, 3*time.Minute, 3*time.Second).Should(Equal("True"))

			By("verifying no objects were uploaded under the dry-run prefix")
			objects, err := listS3Objects(bucketName)
			Expect(err).NotTo(HaveOccurred())
			for _, k := range objects {
				Expect(strings.HasPrefix(k, prefix+"/")).To(BeFalse(),
					"dry-run should not upload to S3, but found %q", k)
			}
		})
	})

	// -----------------------------------------------------------------
	// zstd compression
	// -----------------------------------------------------------------
	Describe("COMPRESSION_TYPE=zstd", Ordered, func() {
		const (
			scheduleName = "zstd-e2e"
			job          = "zstd-e2e-manual"
			prefix       = "zstd-e2e"
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
  compression: zstd
`, scheduleName, testNamespace, dumpscriptImg, testNamespace, bucketName, prefix, localstackInCluster)

		BeforeAll(func() {
			applyManifest(schedule)
		})

		AfterAll(func() {
			runOutput("kubectl", "delete", "backupschedule", scheduleName, //nolint:errcheck
				"-n", testNamespace, "--ignore-not-found")
		})

		It("backup uploads a .zst object (not .gz)", func() {
			By("triggering manual Job")
			run("kubectl", "create", "job", job,
				"--from=cronjob/"+scheduleName, "-n", testNamespace)

			By("waiting for Job to complete")
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
						"-l", "job-name="+job, "-n", testNamespace, "--tail=80")
					return "Failed", fmt.Errorf("zstd backup job failed:\n%s", logs)
				}
				return "", nil
			}, 5*time.Minute, 3*time.Second).Should(Equal("Complete"))

			By("verifying a .zst object exists under the prefix and no .gz")
			Eventually(func() ([]string, error) {
				return listS3Objects(bucketName)
			}).Should(Not(BeEmpty()))

			objects, _ := listS3Objects(bucketName)
			var foundZst, foundGzInPrefix bool
			for _, k := range objects {
				if !strings.HasPrefix(k, prefix+"/") {
					continue
				}
				if strings.HasSuffix(k, ".zst") {
					foundZst = true
				}
				if strings.HasSuffix(k, ".gz") {
					foundGzInPrefix = true
				}
			}
			Expect(foundZst).To(BeTrue(),
				"expected at least one .zst object under %s/, got: %v", prefix, objects)
			Expect(foundGzInPrefix).To(BeFalse(),
				"COMPRESSION_TYPE=zstd should not produce .gz objects, got: %v", objects)
		})
	})

	// -----------------------------------------------------------------
	// Object tagging on S3
	// -----------------------------------------------------------------
	Describe("S3 object tagging", func() {
		It("dump objects carry managed_by + engine + periodicity tags", func() {
			By("listing existing objects under the canonical postgres prefix")
			objects, err := listS3Objects(bucketName)
			Expect(err).NotTo(HaveOccurred())

			var key string
			for _, k := range objects {
				if strings.HasPrefix(k, "main-e2e/daily/") &&
					(strings.HasSuffix(k, ".gz") || strings.HasSuffix(k, ".zst")) {
					key = k
					break
				}
			}
			Expect(key).NotTo(BeEmpty(),
				"this spec needs a postgres backup from the main suite; objects=%v", objects)

			By("fetching tag set via the LocalStack ?tagging endpoint")
			tags, err := getS3ObjectTags(bucketName, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(tags["managed_by"]).To(Equal("dumpscript"),
				"managed_by tag missing/wrong; tags=%v", tags)
			Expect(tags["engine"]).To(Equal("postgresql"),
				"engine tag missing/wrong; tags=%v", tags)
			Expect(tags["periodicity"]).To(Equal("daily"),
				"periodicity tag missing/wrong; tags=%v", tags)
		})
	})
})

// s3TagSet is the minimal subset of the GetObjectTagging XML response we need.
type s3TagSet struct {
	XMLName xml.Name `xml:"Tagging"`
	TagSet  struct {
		Tag []struct {
			Key   string `xml:"Key"`
			Value string `xml:"Value"`
		} `xml:"Tag"`
	} `xml:"TagSet"`
}

// getS3ObjectTags fetches an object's tag set via LocalStack's
// ?tagging endpoint. LocalStack accepts unsigned reads on the test bucket so
// a plain GET is enough — no need to recreate the AWS SigV4 dance from
// seedS3Object().
func getS3ObjectTags(bucket, key string) (map[string]string, error) {
	url := fmt.Sprintf("http://localhost:%s/%s/%s?tagging", lsLocalPort, bucket, key)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GetObjectTagging %s: HTTP %d", key, resp.StatusCode)
	}
	var parsed s3TagSet
	if err := xml.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode tagging xml: %w", err)
	}
	out := make(map[string]string, len(parsed.TagSet.Tag))
	for _, t := range parsed.TagSet.Tag {
		out[t.Key] = t.Value
	}
	return out, nil
}
