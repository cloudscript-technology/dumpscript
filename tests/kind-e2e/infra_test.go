//go:build kind_e2e

package kinde2e_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// portForwardProc holds the kubectl port-forward processes so AfterSuite can kill them.
var (
	portForwardProc    *exec.Cmd
	gcsPortForwardProc *exec.Cmd
)

var _ = BeforeSuite(func() {
	SetDefaultEventuallyTimeout(3 * time.Minute)
	SetDefaultEventuallyPollingInterval(3 * time.Second)

	By("checking required tools")
	requireTools("kind", "kubectl", "docker", "terragrunt", "aws")

	By("cleaning up any leftover kind cluster from a previous run")
	exec.Command("kind", "delete", "cluster", "--name", clusterName).Run() //nolint:errcheck

	By("removing stale Terraform state")
	os.Remove("/tmp/dumpscript-kind-e2e.tfstate")
	os.Remove("/tmp/dumpscript-kind-e2e.tfstate.backup")

	By("creating kind cluster")
	run("kind", "create", "cluster", "--name", clusterName, "--wait", "120s")
	run("kubectl", "config", "use-context", "kind-"+clusterName)

	By("creating test namespace")
	run("kubectl", "create", "namespace", testNamespace)

	manifests := filepath.Join(kindE2EDir, "manifests")

	By("deploying LocalStack")
	run("kubectl", "apply", "-f", filepath.Join(manifests, "localstack.yaml"), "-n", testNamespace)
	run("kubectl", "rollout", "status", "deployment/localstack", "-n", testNamespace, "--timeout=120s")

	By("deploying PostgreSQL")
	run("kubectl", "apply", "-f", filepath.Join(manifests, "postgres.yaml"), "-n", testNamespace)
	run("kubectl", "rollout", "status", "deployment/postgres", "-n", testNamespace, "--timeout=120s")

	By("deploying fake-gcs-server (GCS emulator)")
	run("kubectl", "apply", "-f", filepath.Join(manifests, "fake-gcs.yaml"), "-n", testNamespace)
	run("kubectl", "rollout", "status", "deployment/fake-gcs", "-n", testNamespace, "--timeout=120s")

	By(fmt.Sprintf("port-forwarding LocalStack → localhost:%s", lsLocalPort))
	portForwardProc = exec.Command("kubectl", "port-forward",
		"svc/localstack", lsLocalPort+":4566",
		"-n", testNamespace)
	portForwardProc.Env = podmanEnv()
	portForwardProc.Stdout = io.Discard
	portForwardProc.Stderr = io.Discard
	Expect(portForwardProc.Start()).To(Succeed(), "failed to start kubectl port-forward")

	By("waiting for LocalStack to be healthy")
	waitForURL("http://localhost:"+lsLocalPort+"/_localstack/health", 2*time.Minute)

	By(fmt.Sprintf("port-forwarding fake-gcs-server → localhost:%s", gcsLocalPort))
	gcsPortForwardProc = exec.Command("kubectl", "port-forward",
		"svc/fake-gcs", gcsLocalPort+":4443",
		"-n", testNamespace)
	gcsPortForwardProc.Env = podmanEnv()
	gcsPortForwardProc.Stdout = io.Discard
	gcsPortForwardProc.Stderr = io.Discard
	Expect(gcsPortForwardProc.Start()).To(Succeed(), "failed to start fake-gcs port-forward")

	By("waiting for fake-gcs-server to be healthy")
	waitForURL("http://localhost:"+gcsLocalPort+"/storage/v1/b", 2*time.Minute)

	By("creating GCS bucket via fake-gcs-server REST API")
	createGCSBucket(gcsBucketName)

	By("provisioning S3 bucket via Terragrunt")
	runTerragrunt("apply", "-auto-approve")

	By("building dumpscript image")
	runIn(projectRoot, "docker", "build", "-f", "docker/Dockerfile", "-t", dumpscriptImg, ".")
	kindLoadImage(dumpscriptImg, clusterName)

	By("building operator image")
	operatorDir := filepath.Join(projectRoot, "operator")
	runIn(operatorDir, "docker", "build", "-t", operatorImg, ".")
	kindLoadImage(operatorImg, clusterName)

	By("installing CRDs")
	crdDir := filepath.Join(projectRoot, "operator", "config", "crd", "bases")
	run("kubectl", "apply", "-f", crdDir)

	By("deploying operator via kustomize (image override via sed)")
	deployOperator(filepath.Join(projectRoot, "operator", "config", "default"))

	By("waiting for operator pod to be Running")
	Eventually(func(g Gomega) {
		phase, _ := runOutput("kubectl", "get", "pods",
			"-l", "control-plane=controller-manager",
			"-n", operatorNS,
			"-o", "jsonpath={.items[0].status.phase}")
		if phase != "Running" {
			events, _ := runOutput("kubectl", "get", "events",
				"-n", operatorNS, "--sort-by=.lastTimestamp",
				"--field-selector=involvedObject.kind=Pod")
			GinkgoWriter.Printf("operator pod phase=%q events:\n%s\n", phase, events)
		}
		g.Expect(phase).To(Equal("Running"))
	}, 3*time.Minute, 5*time.Second).Should(Succeed())

	By("creating AWS credentials secret (used by static-credential test specs)")
	run("kubectl", "create", "secret", "generic", "aws-credentials",
		"--from-literal=AWS_ACCESS_KEY_ID=test",
		"--from-literal=AWS_SECRET_ACCESS_KEY=test",
		"-n", testNamespace)

	By("setting up IRSA: ServiceAccount + LocalStack IAM role (for IRSA test spec)")
	setupIRSA()

	By("creating PostgreSQL credentials secret")
	run("kubectl", "create", "secret", "generic", "postgres-credentials",
		"--from-literal=username=testuser",
		"--from-literal=password=testpassword",
		"-n", testNamespace)
})

var _ = AfterSuite(func() {
	// Destroy S3 bucket while LocalStack is still reachable via port-forward.
	By("destroying S3 bucket via Terragrunt")
	c := exec.Command("terragrunt", "destroy", "-auto-approve")
	c.Dir = kindE2EDir
	c.Env = append(podmanEnv(),
		"TF_VAR_localstack_endpoint=http://localhost:"+lsLocalPort,
		"TF_VAR_bucket_name="+bucketName,
	)
	c.Run() //nolint:errcheck

	By("stopping port-forwards")
	if portForwardProc != nil && portForwardProc.Process != nil {
		portForwardProc.Process.Kill() //nolint:errcheck
		portForwardProc.Wait()         //nolint:errcheck
	}
	if gcsPortForwardProc != nil && gcsPortForwardProc.Process != nil {
		gcsPortForwardProc.Process.Kill() //nolint:errcheck
		gcsPortForwardProc.Wait()         //nolint:errcheck
	}

	By("deleting kind cluster")
	exec.Command("kind", "delete", "cluster", "--name", clusterName).Run() //nolint:errcheck
})

// setupIRSA configures the kind e2e namespace to use ServiceAccount-based auth
// instead of static AWS credentials:
//
//  1. Registers the kind cluster's OIDC issuer with LocalStack IAM so that
//     sts:AssumeRoleWithWebIdentity succeeds.
//  2. Creates an IAM role in LocalStack that the ServiceAccount can assume.
//  3. Creates a Kubernetes ServiceAccount annotated with the role ARN.
//
// BackupSchedule specs then use `serviceAccountName: dumpscript-sa` and
// `roleARN` — no credentialsSecretRef needed.
func setupIRSA() {
	GinkgoHelper()

	// Discover the OIDC issuer URL from the kind API server.
	issuerJSON := mustOutput("kubectl", "get", "--raw",
		"/.well-known/openid-configuration")
	// Extract the "issuer" value from the JSON without importing encoding/json.
	// The discovery doc looks like: {"issuer":"https://...", ...}
	const issuerKey = `"issuer":"`
	idx := strings.Index(issuerJSON, issuerKey)
	Expect(idx).To(BeNumerically(">=", 0),
		"could not find issuer field in OIDC discovery: %s", issuerJSON[:min(200, len(issuerJSON))])
	rest := issuerJSON[idx+len(issuerKey):]
	end := strings.IndexByte(rest, '"')
	Expect(end).To(BeNumerically(">", 0), "malformed OIDC issuer JSON")
	issuer := rest[:end]
	GinkgoWriter.Printf("IRSA setup: OIDC issuer = %s\n", issuer)

	// Register the cluster's OIDC provider in LocalStack (thumbprint is not
	// validated in community edition — any 40-char hex string is accepted).
	iamEndpoint := fmt.Sprintf("http://localhost:%s", lsLocalPort)
	fakeThumbprint := "9e99a48a9960b14926bb7f3b02e22da2b0ab7280"
	runAWSCLI(iamEndpoint,
		"iam", "create-open-id-connect-provider",
		"--url", issuer,
		"--thumbprint-list", fakeThumbprint,
		"--client-id-list", "sts.amazonaws.com",
	)

	// Strip the scheme from the issuer URL to build the OIDC provider ARN path
	// used in the trust policy condition.
	issuerHost := strings.TrimPrefix(strings.TrimPrefix(issuer, "https://"), "http://")
	trustPolicy := fmt.Sprintf(
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Federated":"arn:aws:iam::000000000000:oidc-provider/%s"},"Action":"sts:AssumeRoleWithWebIdentity","Condition":{"StringEquals":{"%s:aud":"sts.amazonaws.com"}}}]}`,
		issuerHost, issuerHost)

	runAWSCLI(iamEndpoint,
		"iam", "create-role",
		"--role-name", irsaRoleName,
		"--assume-role-policy-document", trustPolicy,
	)
	runAWSCLI(iamEndpoint,
		"iam", "attach-role-policy",
		"--role-name", irsaRoleName,
		"--policy-arn", "arn:aws:iam::aws:policy/AmazonS3FullAccess",
	)

	// Create the Kubernetes ServiceAccount annotated with the role ARN.
	roleARN := fmt.Sprintf("arn:aws:iam::000000000000:role/%s", irsaRoleName)
	applyManifest(fmt.Sprintf(`
apiVersion: v1
kind: ServiceAccount
metadata:
  name: %s
  namespace: %s
  annotations:
    eks.amazonaws.com/role-arn: "%s"
`, irsaSAName, testNamespace, roleARN))

	GinkgoWriter.Printf("IRSA setup complete: SA=%s role=%s\n", irsaSAName, roleARN)
}

// runAWSCLI invokes the aws CLI targeting the given endpoint-url using the
// fake LocalStack credentials. Fails the test if the command returns non-zero.
func runAWSCLI(endpointURL string, args ...string) {
	GinkgoHelper()
	full := append([]string{
		"--endpoint-url", endpointURL,
		"--region", "us-east-1",
		"--output", "json",
	}, args...)
	c := exec.Command("aws", full...)
	c.Env = append(podmanEnv(),
		"AWS_ACCESS_KEY_ID=test",
		"AWS_SECRET_ACCESS_KEY=test",
		"AWS_DEFAULT_REGION=us-east-1",
	)
	out, err := c.CombinedOutput()
	// Ignore "already exists" errors — idempotent setup.
	if err != nil && !strings.Contains(string(out), "EntityAlreadyExists") {
		Expect(err).NotTo(HaveOccurred(),
			"aws %v failed:\n%s", args, string(out))
	}
}

// deployOperator runs kubectl kustomize on kustomizeDir, replaces the placeholder
// image with operatorImg, sets imagePullPolicy: IfNotPresent (so kind uses the
// locally loaded image), and applies the result.
func deployOperator(kustomizeDir string) {
	GinkgoHelper()
	kc := exec.Command("kubectl", "kustomize", kustomizeDir)
	kc.Env = podmanEnv()
	raw, err := kc.Output()
	Expect(err).NotTo(HaveOccurred(), "kubectl kustomize %s failed", kustomizeDir)

	patched := string(raw)
	patched = strings.ReplaceAll(patched, "controller:latest", operatorImg)
	// Ensure the operator pod uses the locally loaded image and does not attempt
	// to pull from a registry (which would fail in a kind environment).
	patched = strings.ReplaceAll(patched, "image: "+operatorImg,
		"image: "+operatorImg+"\n        imagePullPolicy: IfNotPresent")

	apply := exec.Command("kubectl", "apply", "-f", "-")
	apply.Stdin = bytes.NewBufferString(patched)
	apply.Env = podmanEnv()
	out, err := apply.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "kubectl apply operator manifests failed:\n%s", string(out))
}

// kindLoadImage saves the image with podman and pipes it directly into
// containerd inside the kind node container. This bypasses the kind load
// machinery and is reliable with rootless podman on NixOS.
func kindLoadImage(img, cluster string) {
	GinkgoHelper()
	GinkgoWriter.Printf("loading image %s into kind cluster %s\n", img, cluster)

	// Determine the kind node container name (always <cluster>-control-plane).
	nodeName := cluster + "-control-plane"

	// Save the image from podman and import it directly into the kind node's
	// containerd. `ctr` is always available inside kindest/node images.
	save := exec.Command("podman", "save", img)
	save.Env = podmanEnv()
	saveOut, err := save.Output()
	Expect(err).NotTo(HaveOccurred(), "podman save %s failed", img)

	// Import via containerd's ctr tool inside the kind node container.
	// We use `podman exec` because the cluster was created with podman.
	imp := exec.Command("podman", "exec", "-i", nodeName,
		"ctr", "--namespace=k8s.io", "images", "import", "--all-platforms", "-")
	imp.Stdin = bytes.NewReader(saveOut)
	imp.Env = podmanEnv()
	out, err := imp.CombinedOutput()
	Expect(err).NotTo(HaveOccurred(), "ctr images import %s failed:\n%s", img, string(out))
	GinkgoWriter.Printf("loaded %s: %s\n", img, strings.TrimSpace(string(out)))
}
