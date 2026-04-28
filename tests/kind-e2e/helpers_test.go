//go:build kind_e2e

package kinde2e_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// run executes a command and fails the test on error.
func run(name string, args ...string) {
	GinkgoHelper()
	out, err := cmd(name, args...).CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "$ %s %s\n%s", name, strings.Join(args, " "), string(out))
}

// runIn executes a command in dir and fails the test on error.
func runIn(dir, name string, args ...string) {
	GinkgoHelper()
	c := cmd(name, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "$ %s %s (in %s)\n%s", name, strings.Join(args, " "), dir, string(out))
}

// runOutput executes a command and returns stdout+stderr trimmed.
func runOutput(name string, args ...string) (string, error) {
	out, err := cmd(name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("$ %s %s: %w\n%s", name, strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// mustOutput executes a command and fails the test if it errors.
func mustOutput(name string, args ...string) string {
	GinkgoHelper()
	out, err := runOutput(name, args...)
	Expect(err).NotTo(HaveOccurred())
	return out
}

func cmd(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.Env = podmanEnv()
	return c
}

// podmanEnv returns os.Environ() extended with podman-as-docker variables so
// that kind, docker build, and kind load all go through the podman socket.
// When a real docker daemon is present these vars are benign.
func podmanEnv() []string {
	env := os.Environ()
	// Only inject when docker binary is actually podman (version strings match).
	dockerVer, _ := exec.Command("docker", "--version").Output()
	podmanVer, _ := exec.Command("podman", "--version").Output()
	if strings.Contains(string(dockerVer), "podman") ||
		string(dockerVer) == string(podmanVer) ||
		os.Getenv("KIND_EXPERIMENTAL_PROVIDER") == "podman" {
		uid := fmt.Sprintf("%d", os.Getuid())
		sock := fmt.Sprintf("unix:///run/user/%s/podman/podman.sock", uid)
		env = appendIfMissing(env, "DOCKER_HOST", sock)
		env = appendIfMissing(env, "KIND_EXPERIMENTAL_PROVIDER", "podman")
	}
	return env
}

func appendIfMissing(env []string, key, val string) []string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return env // already set by caller
		}
	}
	return append(env, prefix+val)
}

// requireTools fails the test if any of the given executables are not in PATH.
func requireTools(tools ...string) {
	GinkgoHelper()
	for _, tool := range tools {
		_, err := exec.LookPath(tool)
		Expect(err).NotTo(HaveOccurred(), "required tool not found in PATH: %s — please install it before running kind e2e tests", tool)
	}
}

// runTerragrunt runs terragrunt in the kind-e2e directory with the right env vars.
func runTerragrunt(args ...string) {
	GinkgoHelper()
	c := exec.Command("terragrunt", args...)
	c.Dir = kindE2EDir
	c.Env = append(podmanEnv(),
		"TF_VAR_localstack_endpoint=http://localhost:"+lsLocalPort,
		"TF_VAR_bucket_name="+bucketName,
	)
	out, err := c.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "terragrunt %v failed:\n%s", args, string(out))
}

// waitForURL polls url until it returns a non-5xx response or timeout expires.
func waitForURL(url string, timeout time.Duration) {
	GinkgoHelper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	Fail(fmt.Sprintf("URL %s not ready after %s: %v", url, timeout, lastErr))
}

// s3ListResult is a minimal S3 ListObjectsV2 XML response.
type s3ListResult struct {
	XMLName  xml.Name `xml:"ListBucketResult"`
	Contents []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
}

// listS3Objects returns the keys of all objects in bucket via the LocalStack HTTP API.
func listS3Objects(bucket string) ([]string, error) {
	url := fmt.Sprintf("http://localhost:%s/%s?list-type=2", lsLocalPort, bucket)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result s3ListResult
	if err := xml.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("xml parse: %w\nbody: %s", err, string(body))
	}
	keys := make([]string, len(result.Contents))
	for i, c := range result.Contents {
		keys[i] = c.Key
	}
	return keys, nil
}

// applyManifest runs kubectl apply -f - with content as stdin.
func applyManifest(content string) {
	GinkgoHelper()
	c := exec.Command("kubectl", "apply", "-f", "-")
	c.Stdin = strings.NewReader(content)
	out, err := c.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "kubectl apply failed:\n%s", string(out))
}

// kubectlExec runs a command inside a pod container and returns the trimmed output.
func kubectlExec(namespace, pod, container string, command ...string) string {
	GinkgoHelper()
	args := append([]string{"exec", pod, "-n", namespace, "-c", container, "--"}, command...)
	out, err := runOutput("kubectl", args...)
	Expect(err).NotTo(HaveOccurred())
	return out
}

// pgPodName returns the name of the first running postgres pod in testNamespace.
func pgPodName() string {
	GinkgoHelper()
	return mustOutput("kubectl", "get", "pod",
		"-l", "app=postgres",
		"-n", testNamespace,
		"-o", "jsonpath={.items[0].metadata.name}")
}

// seedS3Object PUTs an empty object at key in the test bucket via the
// LocalStack port-forward. Uses a manually constructed AWS Signature V4 so no
// SDK dependency is needed and no extra pod has to be pulled into the cluster.
func seedS3Object(key string) {
	GinkgoHelper()

	now := time.Now().UTC()
	dateTime := now.Format("20060102T150405Z")
	date := now.Format("20060102")

	const (
		region       = "us-east-1"
		service      = "s3"
		accessKey    = "test"
		secretKey    = "test"
		payloadHash  = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // sha256("")
	)

	host := "localhost:" + lsLocalPort
	urlPath := "/" + bucketName + "/" + key

	canonicalHeaders := "host:" + host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + dateTime + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		"PUT", urlPath, "",
		canonicalHeaders, signedHeaders, payloadHash,
	}, "\n")

	credScope := date + "/" + region + "/" + service + "/aws4_request"
	crHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + dateTime + "\n" + credScope + "\n" +
		hex.EncodeToString(crHash[:])

	mac := func(key, msg []byte) []byte {
		h := hmac.New(sha256.New, key)
		h.Write(msg)
		return h.Sum(nil)
	}
	sigKey := mac(mac(mac(mac([]byte("AWS4"+secretKey), []byte(date)), []byte(region)), []byte(service)), []byte("aws4_request"))
	sig := hex.EncodeToString(mac(sigKey, []byte(stringToSign)))

	auth := "AWS4-HMAC-SHA256 Credential=" + accessKey + "/" + credScope +
		",SignedHeaders=" + signedHeaders + ",Signature=" + sig

	req, err := http.NewRequest("PUT", "http://"+host+urlPath, http.NoBody)
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set("x-amz-date", dateTime)
	req.Header.Set("x-amz-content-sha256", payloadHash)
	req.Header.Set("Authorization", auth)
	req.ContentLength = 0

	resp, err := http.DefaultClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(BeNumerically("<", 300),
		"seed S3 object %q: HTTP %d", key, resp.StatusCode)
}

// irsaS3Storage returns a YAML fragment for an S3 storage block that uses
// IRSA (ServiceAccount token) instead of static credentialsSecretRef.
// The dumpscript pod will call LocalStack STS to exchange the projected SA
// token for temporary credentials before making S3 requests.
func irsaS3Storage(bucket, prefix string) string {
	return fmt.Sprintf(`  storage:
    backend: s3
    s3:
      bucket: %s
      prefix: "%s"
      region: us-east-1
      endpointURL: %s
      roleARN: "%s"
`, bucket, prefix, localstackInCluster, irsaRoleARN)
}

// psql runs a SQL statement via psql inside the postgres pod.
func psql(sql string) string {
	GinkgoHelper()
	return kubectlExec(testNamespace, pgPodName(), "postgres",
		"psql", "-U", "testuser", "-d", "testdb", "-t", "-c", sql)
}
