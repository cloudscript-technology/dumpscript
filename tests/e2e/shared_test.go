//go:build e2e

// Package e2e contains end-to-end integration tests for the dumpscript image,
// gated by the `e2e` build tag so they don't run in `go test ./...`.
//
// Usage:
//
//	go test -tags=e2e -v ./tests/e2e/...
//
// Requires a running Docker- or Podman-compatible daemon (DOCKER_HOST). On
// macOS + podman, first run `podman machine start` and export the socket:
//
//	export DOCKER_HOST="unix://$(podman machine inspect --format \
//	   '{{.ConnectionInfo.PodmanSocket.Path}}')"
package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcnetwork "github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	sharedNet        *testcontainers.DockerNetwork
	sharedMinIO      testcontainers.Container
	sharedMinIOEndpt string
	minioAlias       = "e2e-minio"
	minioUser        = "admin"
	minioPass        = "adminadmin"
	// Use full local registry prefix so podman doesn't try docker.io pull.
	// Override with E2E_IMAGE env if your daemon stores it elsewhere.
	dumpscriptImage = "localhost/dumpscript:go-alpine"
)

func TestMain(m *testing.M) {
	if v := os.Getenv("E2E_IMAGE"); v != "" {
		dumpscriptImage = v
	}
	ctx := context.Background()

	var err error
	sharedNet, err = tcnetwork.New(ctx)
	if err != nil {
		panic(fmt.Errorf("create network: %w", err))
	}

	sharedMinIO, err = testcontainers.Run(ctx, "minio/minio:latest",
		testcontainers.WithEnv(map[string]string{
			"MINIO_ROOT_USER":     minioUser,
			"MINIO_ROOT_PASSWORD": minioPass,
		}),
		testcontainers.WithCmd("server", "/data"),
		testcontainers.WithExposedPorts("9000/tcp"),
		tcnetwork.WithNetwork([]string{minioAlias}, sharedNet),
		testcontainers.WithWaitStrategy(
			wait.ForHTTP("/minio/health/ready").WithPort("9000/tcp").
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		_ = sharedNet.Remove(ctx)
		panic(fmt.Errorf("start minio: %w", err))
	}

	host, _ := sharedMinIO.Host(ctx)
	port, _ := sharedMinIO.MappedPort(ctx, "9000/tcp")
	sharedMinIOEndpt = net.JoinHostPort(host, port.Port())

	code := m.Run()

	_ = sharedMinIO.Terminate(ctx)
	_ = sharedNet.Remove(ctx)
	os.Exit(code)
}

// s3client returns a MinIO client connected to the host-mapped port.
func s3client(t *testing.T) *minio.Client {
	t.Helper()
	c, err := minio.New(sharedMinIOEndpt, &minio.Options{
		Creds:  credentials.NewStaticV4(minioUser, minioPass, ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("minio client: %v", err)
	}
	return c
}

func createBucket(t *testing.T, name string) {
	t.Helper()
	ctx := context.Background()
	c := s3client(t)
	exists, err := c.BucketExists(ctx, name)
	if err != nil {
		t.Fatalf("bucket exists %s: %v", name, err)
	}
	if !exists {
		if err := c.MakeBucket(ctx, name, minio.MakeBucketOptions{}); err != nil {
			t.Fatalf("make bucket %s: %v", name, err)
		}
	}
}

func listKeys(t *testing.T, bucket string) []string {
	t.Helper()
	ctx := context.Background()
	c := s3client(t)
	var out []string
	for obj := range c.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err != nil {
			t.Fatalf("list objects: %v", obj.Err)
		}
		out = append(out, obj.Key)
	}
	return out
}

func putObject(t *testing.T, bucket, key, body string) {
	t.Helper()
	ctx := context.Background()
	c := s3client(t)
	_, err := c.PutObject(ctx, bucket, key,
		strings.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{})
	if err != nil {
		t.Fatalf("put %s/%s: %v", bucket, key, err)
	}
}

// firstKeyWithSuffix returns the first key ending in suffix, or "".
func firstKeyWithSuffix(keys []string, suffix string) string {
	for _, k := range keys {
		if strings.HasSuffix(k, suffix) {
			return k
		}
	}
	return ""
}

// dumpscriptRun describes one invocation of the dumpscript image.
type dumpscriptRun struct {
	Subcmd string
	Env    map[string]string
}

// runDumpscript runs the dumpscript image inside sharedNet, waits for exit,
// returns logs + exit code.
func runDumpscript(t *testing.T, r dumpscriptRun) (logs string, exitCode int) {
	t.Helper()
	ctx := context.Background()

	env := map[string]string{
		"STORAGE_BACKEND":       "s3",
		"AWS_REGION":            "us-east-1",
		"AWS_ACCESS_KEY_ID":     minioUser,
		"AWS_SECRET_ACCESS_KEY": minioPass,
		"AWS_S3_ENDPOINT_URL":   fmt.Sprintf("http://%s:9000", minioAlias),
		"PERIODICITY":           "daily",
		"RETENTION_DAYS":        "7",
		"LOG_LEVEL":             "info",
	}
	for k, v := range r.Env {
		env[k] = v
	}

	ctr, err := testcontainers.Run(ctx, dumpscriptImage,
		testcontainers.WithEnv(env),
		testcontainers.WithCmd(r.Subcmd),
		tcnetwork.WithNetwork(nil, sharedNet),
		testcontainers.WithWaitStrategy(wait.ForExit().WithExitTimeout(120*time.Second)),
	)
	if err != nil {
		t.Fatalf("start dumpscript (%s): %v", r.Subcmd, err)
	}
	defer func() { _ = ctr.Terminate(ctx) }()

	state, err := ctr.State(ctx)
	if err != nil {
		t.Fatalf("container state: %v", err)
	}
	exitCode = state.ExitCode

	rc, err := ctr.Logs(ctx)
	if err == nil {
		buf, _ := io.ReadAll(rc)
		_ = rc.Close()
		logs = string(buf)
	}
	return logs, exitCode
}

func assertDumpscriptOK(t *testing.T, subcmd string, logs string, exitCode int) {
	t.Helper()
	if exitCode != 0 {
		t.Fatalf("dumpscript %s exit=%d; logs:\n%s", subcmd, exitCode, logs)
	}
}

// runWithinContainer runs `cmd` inside the given container, returns stdout.
// Uses tcexec.Multiplexed() to strip Docker's 8-byte stream framing.
func runWithinContainer(t *testing.T, ctr testcontainers.Container, cmd ...string) string {
	t.Helper()
	ctx := context.Background()
	code, reader, err := ctr.Exec(ctx, cmd, tcexec.Multiplexed())
	if err != nil {
		t.Fatalf("exec %v: %v", cmd, err)
	}
	buf, _ := io.ReadAll(reader)
	if code != 0 {
		t.Fatalf("exec %v exit=%d stdout=%s", cmd, code, buf)
	}
	return string(buf)
}
