package storage

import (
	"context"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestS3_DisplayPath(t *testing.T) {
	cfg := &config.Config{Backend: config.BackendS3, S3: config.S3{Bucket: "my-bucket"}, Upload: config.Upload{ChunkSize: "100M", Concurrency: 4}}
	s, err := NewS3(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	got := s.DisplayPath("dumps/daily/2025/03/24/dump.sql.gz")
	want := "s3://my-bucket/dumps/daily/2025/03/24/dump.sql.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestS3_New_WithStaticCreds(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendS3,
		S3: config.S3{
			Bucket: "b", Region: "us-east-1",
			AccessKeyID: "AKIA", SecretAccessKey: "secret",
		},
		Upload: config.Upload{ChunkSize: "100M", Concurrency: 4},
	}
	s, err := NewS3(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}
	if s == nil {
		t.Fatal("nil client")
	}
}

func TestS3_New_BadChunkSize(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendS3,
		S3:      config.S3{Bucket: "b"},
		Upload:  config.Upload{ChunkSize: "not-a-size", Concurrency: 4},
	}
	_, err := NewS3(context.Background(), cfg, quietLogger())
	if err == nil {
		t.Fatal("expected error for invalid chunk size")
	}
}

func TestS3_New_GCSEndpoint(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendS3,
		S3: config.S3{
			Bucket: "b", EndpointURL: "https://storage.googleapis.com",
		},
		Upload: config.Upload{ChunkSize: "100M", Concurrency: 4},
	}
	s, err := NewS3(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewS3 (GCS endpoint): %v", err)
	}
	if s == nil {
		t.Fatal("nil client")
	}
}

func TestS3_New_MinIOEndpoint(t *testing.T) {
	cfg := &config.Config{
		Backend: config.BackendS3,
		S3: config.S3{
			Bucket: "b", EndpointURL: "https://minio.local:9000",
		},
		Upload: config.Upload{ChunkSize: "100M", Concurrency: 4},
	}
	s, err := NewS3(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewS3 (MinIO endpoint): %v", err)
	}
	if s == nil {
		t.Fatal("nil client")
	}
}
