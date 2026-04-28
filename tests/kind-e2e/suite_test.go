//go:build kind_e2e

package kinde2e_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	clusterName   = "dumpscript-e2e"
	testNamespace = "dumpscript-e2e"
	// Use the localhost/ prefix so containerd finds the image without pulling.
	// Podman (used as docker on this host) stores local images under localhost/.
	dumpscriptImg = "localhost/dumpscript:kind-e2e"
	operatorImg   = "localhost/dumpscript-operator:kind-e2e"
	lsLocalPort   = "14566"
	bucketName    = "dumpscript-e2e"
	operatorNS    = "dumpscript-operator-system"

	// IRSA — ServiceAccount-based auth for LocalStack via sts:AssumeRoleWithWebIdentity.
	irsaSAName   = "dumpscript-sa"
	irsaRoleName = "dumpscript-test"
	// Full LocalStack role ARN: account 000000000000 is LocalStack's fake account.
	irsaRoleARN = "arn:aws:iam::000000000000:role/" + irsaRoleName
	// LocalStack STS endpoint reachable from inside the kind cluster.
	irsaSTSEndpoint = "http://localstack." + testNamespace + ".svc.cluster.local:4566"

	// GCS — fake-gcs-server emulator deployed in the kind cluster.
	gcsBucketName = "dumpscript-gcs-e2e"
	gcsLocalPort  = "14443" // port-forward from fake-gcs-server :4443
	// fake-gcs-server endpoint reachable from inside the kind cluster.
	fakeGCSInCluster = "http://fake-gcs." + testNamespace + ".svc.cluster.local:4443"

	// Azure — Azurite emulator deployed in the kind cluster.
	azureContainer = "dumpscript-azure-e2e"
	azureLocalPort = "14000" // port-forward from azurite :10000
	azureAccount   = "devstoreaccount1"
	// azuriteKey is the Microsoft-published well-known Azurite emulator key.
	// Public, safe for local emulator use only.
	azuriteKey = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	// Azurite blob endpoint reachable from inside the kind cluster.
	azuriteInCluster = "http://azurite." + testNamespace + ".svc.cluster.local:10000/" + azureAccount
)

var (
	projectRoot string
	kindE2EDir  string
)

func TestKindE2E(t *testing.T) {
	// go test sets CWD to the package directory; support override for edge cases.
	if pr := os.Getenv("PROJECT_ROOT"); pr != "" {
		projectRoot = pr
		kindE2EDir = filepath.Join(pr, "tests", "kind-e2e")
	} else {
		var err error
		kindE2EDir, err = os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		projectRoot = filepath.Join(kindE2EDir, "..", "..")
	}
	projectRoot, _ = filepath.Abs(projectRoot)
	kindE2EDir, _ = filepath.Abs(kindE2EDir)

	RegisterFailHandler(Fail)
	RunSpecs(t, "Kind E2E Suite")
}
