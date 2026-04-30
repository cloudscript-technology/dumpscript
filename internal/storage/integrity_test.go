package storage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// fakeS3Server emulates the subset of S3 the SDK exercises during a single
// (non-multipart) Upload + HeadObject. The reported Content-Length on HEAD
// is configurable so tests can simulate a truncation mismatch.
type fakeS3Server struct {
	srv            *httptest.Server
	storedBody     []byte
	headContentLen int64 // -1 = use real stored length
	puts, heads    atomic.Int64
}

func newFakeS3Server() *fakeS3Server {
	f := &fakeS3Server{headContentLen: -1}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			f.puts.Add(1)
			body := make([]byte, 0, r.ContentLength)
			buf := make([]byte, 32*1024)
			for {
				n, err := r.Body.Read(buf)
				if n > 0 {
					body = append(body, buf[:n]...)
				}
				if err != nil {
					break
				}
			}
			f.storedBody = body
			w.Header().Set("ETag", `"deadbeef"`)
			w.Header().Set("x-amz-checksum-sha256", "ignored")
			w.WriteHeader(http.StatusOK)
		case http.MethodHead:
			f.heads.Add(1)
			length := int64(len(f.storedBody))
			if f.headContentLen >= 0 {
				length = f.headContentLen
			}
			w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
			w.Header().Set("ETag", `"deadbeef"`)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	return f
}

func (f *fakeS3Server) Close()      { f.srv.Close() }
func (f *fakeS3Server) URL() string { return f.srv.URL }

func newFakeS3Client(t *testing.T, endpoint string) *S3 {
	t.Helper()
	cfg := &config.Config{
		Backend: config.BackendS3,
		S3: config.S3{
			Bucket: "test-bucket", Region: "us-east-1",
			AccessKeyID: "AKIATEST", SecretAccessKey: "secret",
			EndpointURL: endpoint,
		},
		Upload: config.Upload{ChunkSize: "100M", Concurrency: 1},
	}
	s, err := NewS3(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	return s
}

func writeTmpFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "dump.sql.gz")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestS3_Upload_IntegrityOK(t *testing.T) {
	srv := newFakeS3Server()
	defer srv.Close()

	s := newFakeS3Client(t, srv.URL())
	path := writeTmpFile(t, "hello e2e world")

	if err := s.Upload(context.Background(), path, "dumps/x.sql.gz"); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if srv.puts.Load() == 0 {
		t.Fatal("expected at least one PUT")
	}
	if srv.heads.Load() == 0 {
		t.Fatal("expected at least one HEAD (post-upload integrity check)")
	}
}

func TestS3_Upload_IntegrityMismatchTruncation(t *testing.T) {
	srv := newFakeS3Server()
	defer srv.Close()

	// Force HEAD to lie about size — simulates a truncated commit.
	srv.headContentLen = 3

	s := newFakeS3Client(t, srv.URL())
	path := writeTmpFile(t, "hello e2e world") // 15 bytes

	err := s.Upload(context.Background(), path, "dumps/x.sql.gz")
	if err == nil {
		t.Fatal("expected integrity-check error, got nil")
	}
	if !strings.Contains(err.Error(), "integrity check failed") {
		t.Errorf("err = %v, want containing 'integrity check failed'", err)
	}
	if !strings.Contains(err.Error(), "local=15") {
		t.Errorf("err = %v, want containing 'local=15'", err)
	}
	if !strings.Contains(err.Error(), "remote=3") {
		t.Errorf("err = %v, want containing 'remote=3'", err)
	}
}

func TestS3_Upload_PropagatesHeadError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			w.Header().Set("ETag", `"x"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodHead:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	s := newFakeS3Client(t, srv.URL)
	path := writeTmpFile(t, "data")
	err := s.Upload(context.Background(), path, "dumps/x.sql.gz")
	if err == nil {
		t.Fatal("expected error from HEAD failure")
	}
	if !strings.Contains(err.Error(), "verify head") {
		t.Errorf("err = %v, want containing 'verify head'", err)
	}
}

// ---------------- Azure ----------------

// fakeAzureServer emulates Azurite-style URLs. Returns 201 Created on PUT
// (UploadFile via block blob commit) and a configurable Content-Length on
// HEAD (GetProperties).
type fakeAzureServer struct {
	srv            *httptest.Server
	storedBody     []byte
	headContentLen int64
	puts, heads    atomic.Int64
}

func newFakeAzureServer() *fakeAzureServer {
	f := &fakeAzureServer{headContentLen: -1}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			f.puts.Add(1)
			body := make([]byte, 0, r.ContentLength)
			buf := make([]byte, 32*1024)
			for {
				n, err := r.Body.Read(buf)
				if n > 0 {
					body = append(body, buf[:n]...)
				}
				if err != nil {
					break
				}
			}
			if len(body) > 0 {
				f.storedBody = body
			}
			w.Header().Set("ETag", `"x"`)
			w.Header().Set("Last-Modified", "Wed, 23 Apr 2026 12:00:00 GMT")
			w.WriteHeader(http.StatusCreated)
		case http.MethodHead:
			f.heads.Add(1)
			length := int64(len(f.storedBody))
			if f.headContentLen >= 0 {
				length = f.headContentLen
			}
			w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
			w.Header().Set("ETag", `"x"`)
			w.Header().Set("Last-Modified", "Wed, 23 Apr 2026 12:00:00 GMT")
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	return f
}

func (f *fakeAzureServer) Close()      { f.srv.Close() }
func (f *fakeAzureServer) URL() string { return f.srv.URL }

func newFakeAzureClient(t *testing.T, endpoint string) *Azure {
	t.Helper()
	cfg := &config.Config{
		Backend: config.BackendAzure,
		Azure: config.Azure{
			Account: "devstoreaccount1", Container: "dumps",
			Key:      "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==",
			Endpoint: fmt.Sprintf("%s/%s", endpoint, "devstoreaccount1"),
		},
		Upload: config.Upload{ChunkSize: "100M", Concurrency: 1},
	}
	a, err := NewAzure(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewAzure: %v", err)
	}
	return a
}

func TestAzure_Upload_IntegrityOK(t *testing.T) {
	srv := newFakeAzureServer()
	defer srv.Close()

	a := newFakeAzureClient(t, srv.URL())
	path := writeTmpFile(t, "hello e2e world")

	if err := a.Upload(context.Background(), path, "dumps/x.sql.gz"); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if srv.puts.Load() == 0 {
		t.Fatal("expected at least one PUT")
	}
	if srv.heads.Load() == 0 {
		t.Fatal("expected GetProperties (HEAD) post-upload")
	}
}

func TestAzure_Upload_IntegrityMismatchTruncation(t *testing.T) {
	srv := newFakeAzureServer()
	defer srv.Close()
	srv.headContentLen = 3

	a := newFakeAzureClient(t, srv.URL())
	path := writeTmpFile(t, "hello e2e world") // 15 bytes

	err := a.Upload(context.Background(), path, "dumps/x.sql.gz")
	if err == nil {
		t.Fatal("expected integrity-check error, got nil")
	}
	if !strings.Contains(err.Error(), "integrity check failed") {
		t.Errorf("err = %v, want containing 'integrity check failed'", err)
	}
	if !strings.Contains(err.Error(), "local=15") {
		t.Errorf("err = %v, want containing 'local=15'", err)
	}
	if !strings.Contains(err.Error(), "remote=3") {
		t.Errorf("err = %v, want containing 'remote=3'", err)
	}
}
