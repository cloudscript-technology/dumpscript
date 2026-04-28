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
