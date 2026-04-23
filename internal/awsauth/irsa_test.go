package awsauth

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestIRSAProvider_NoRoleARN_ReturnsNil(t *testing.T) {
	cfg := &config.Config{S3: config.S3{}}
	p, err := IRSAProvider(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil provider, got %T", p)
	}
}

func TestIRSAProvider_MissingTokenFile_ReturnsNil(t *testing.T) {
	if _, err := os.Stat(DefaultTokenPath); err == nil {
		t.Skip("running inside an environment where the IRSA token is present; skipping")
	}
	cfg := &config.Config{S3: config.S3{RoleARN: "arn:aws:iam::111111111111:role/test"}}
	p, err := IRSAProvider(context.Background(), cfg, quietLogger())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if p != nil {
		t.Errorf("expected nil provider when token missing, got %T", p)
	}
}

func TestDefaultTokenPath_Constant(t *testing.T) {
	const want = "/var/run/secrets/eks.amazonaws.com/serviceaccount/token"
	if DefaultTokenPath != want {
		t.Errorf("DefaultTokenPath = %q, want %q", DefaultTokenPath, want)
	}
}
