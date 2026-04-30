package storage

import (
	"context"
	"os"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestGCS_DisplayPath(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendGCS,
		GCS: config.GCS{
			Bucket:   "my-prod-backups",
			Endpoint: "http://localhost:0/skip-auth",
		},
	}
	g, err := NewGCS(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewGCS: %v", err)
	}
	got := g.DisplayPath("dumps/daily/2025/03/24/dump.sql.gz")
	want := "gs://my-prod-backups/dumps/daily/2025/03/24/dump.sql.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGCS_New_WithEndpointSkipsAuth(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendGCS,
		GCS: config.GCS{
			Bucket:   "b",
			Endpoint: "http://localhost:0/skip-auth",
		},
	}
	g, err := NewGCS(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewGCS with endpoint: %v", err)
	}
	if g == nil {
		t.Fatal("nil client")
	}
}

func TestGCS_New_WithCredentialsFile(t *testing.T) {
	// CredentialsFile and Endpoint(WithoutAuthentication) are mutually
	// exclusive options in the SDK. This test exercises the credentials-file
	// path alone — the client is lazy, so a placeholder file is enough at
	// construction time.
	tmp := t.TempDir() + "/fake-sa.json"
	if err := writeFakeServiceAccount(tmp); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Backend: config.BackendGCS,
		GCS: config.GCS{
			Bucket:          "b",
			CredentialsFile: tmp,
		},
	}
	if _, err := NewGCS(context.Background(), cfg, quietLogger()); err != nil {
		t.Fatalf("NewGCS: %v", err)
	}
}

// writeFakeServiceAccount writes the smallest valid SA JSON the SDK accepts.
// `private_key` is a syntactically valid PEM but not a usable key — fine
// because the client is lazy and we never make a request.
func writeFakeServiceAccount(path string) error {
	const stub = `{
		"type": "service_account",
		"project_id": "test",
		"private_key_id": "00",
		"private_key": "-----BEGIN PRIVATE KEY-----\nMIIBVgIBADANBgkqhkiG9w0BAQEFAASCAUAwggE8AgEAAkEA1tu+0aFpfKEJnFbz\n-----END PRIVATE KEY-----\n",
		"client_email": "test@test.iam.gserviceaccount.com",
		"client_id": "0",
		"auth_uri": "https://accounts.google.com/o/oauth2/auth",
		"token_uri": "https://oauth2.googleapis.com/token"
	}`
	return os.WriteFile(path, []byte(stub), 0o600)
}

func TestGCS_RegisteredInFactory(t *testing.T) {
	got := Registered()
	for _, b := range got {
		if b == config.BackendGCS {
			return
		}
	}
	t.Errorf("BackendGCS not registered; got %v", got)
}

func TestGCS_FactoryNew_WithGCSBackend(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendGCS,
		GCS: config.GCS{
			Bucket:   "b",
			Endpoint: "http://localhost:0/skip-auth",
		},
		Upload: config.Upload{ChunkSize: "100M", Concurrency: 4},
	}
	s, err := New(context.Background(), cfg, quietLogger(), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s == nil {
		t.Fatal("nil storage")
	}
	if got := s.DisplayPath("k"); got != "gs://b/k" {
		t.Errorf("DisplayPath via factory = %q, want gs://b/k", got)
	}
}
