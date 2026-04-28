package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.BackendGCS, func(ctx context.Context, cfg *config.Config, log *slog.Logger, _ Options) (Storage, error) {
		return NewGCS(ctx, cfg, log)
	})
}

// GCS is the Adapter around cloud.google.com/go/storage. Authentication
// uses Application Default Credentials, which resolves automatically via:
//
//   - GOOGLE_APPLICATION_CREDENTIALS file (or GCS_CREDENTIALS_FILE override)
//   - gcloud auth application-default login (local dev)
//   - GKE Workload Identity (production — zero static secrets)
//   - GCE metadata server (Compute Engine, Cloud Run, Cloud Functions)
type GCS struct {
	cfg    *config.Config
	log    *slog.Logger
	client *storage.Client
	bucket *storage.BucketHandle
}

func NewGCS(ctx context.Context, cfg *config.Config, log *slog.Logger) (*GCS, error) {
	var opts []option.ClientOption
	if cfg.GCS.CredentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.GCS.CredentialsFile))
	}
	if cfg.GCS.Endpoint != "" {
		// fake-gcs-server / on-prem GCS-compatible emulator. The Go SDK has
		// special-case handling for STORAGE_EMULATOR_HOST that routes ALL
		// JSON API operations (List, Get, Insert, …) through the emulator
		// and disables authentication automatically. option.WithEndpoint
		// alone misses some paths (notably List).
		//
		// We accept either "host:port" or "http://host:port" — the SDK adds
		// the scheme if missing.
		host := cfg.GCS.Endpoint
		if u, err := url.Parse(host); err == nil && u.Host != "" {
			host = u.Host
		}
		_ = os.Setenv("STORAGE_EMULATOR_HOST", host)
		// Skip option.WithEndpoint here — it conflicts with the emulator path
		// in newer SDK versions. STORAGE_EMULATOR_HOST is sufficient.
	}
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcs client: %w", err)
	}
	return &GCS{
		cfg:    cfg,
		log:    log,
		client: client,
		bucket: client.Bucket(cfg.GCS.Bucket),
	}, nil
}

func (g *GCS) Upload(ctx context.Context, localPath, key string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat local: %w", err)
	}
	localSize := fi.Size()

	w := g.bucket.Object(key).NewWriter(ctx)
	if _, err := io.Copy(w, f); err != nil {
		_ = w.Close()
		return fmt.Errorf("gcs upload copy: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("gcs upload close: %w", err)
	}

	// Post-upload size sanity check — same protection as the S3 / Azure
	// backends. GCS already performs CRC32C end-to-end, so this is the
	// "did the commit complete" safety net.
	attrs, err := g.bucket.Object(key).Attrs(ctx)
	if err != nil {
		return fmt.Errorf("gcs verify attrs: %w", err)
	}
	if attrs.Size != localSize {
		return fmt.Errorf("gcs upload integrity check failed: local=%d remote=%d (key=%s)",
			localSize, attrs.Size, key)
	}
	g.log.Debug("gcs upload integrity verified", "key", key, "size", localSize, "crc32c", attrs.CRC32C)
	return nil
}

func (g *GCS) UploadBytes(ctx context.Context, data []byte, key string) error {
	w := g.bucket.Object(key).NewWriter(ctx)
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func (g *GCS) Exists(ctx context.Context, key string) (bool, error) {
	_, err := g.bucket.Object(key).Attrs(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, storage.ErrObjectNotExist) {
		return false, nil
	}
	return false, err
}

func (g *GCS) Download(ctx context.Context, key, localPath string) error {
	r, err := g.bucket.Object(key).NewReader(ctx)
	if err != nil {
		return fmt.Errorf("gcs download reader: %w", err)
	}
	defer r.Close()

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("gcs download copy: %w", err)
	}
	return nil
}

func (g *GCS) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	it := g.bucket.Objects(ctx, &storage.Query{Prefix: prefix})
	for {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gcs list: %w", err)
		}
		out = append(out, Object{
			Key:      attrs.Name,
			Size:     attrs.Size,
			Modified: attrs.Updated,
		})
	}
	return out, nil
}

func (g *GCS) Delete(ctx context.Context, key string) error {
	return g.bucket.Object(key).Delete(ctx)
}

func (g *GCS) DisplayPath(key string) string {
	u := url.URL{Scheme: "gs", Host: g.cfg.GCS.Bucket, Path: "/" + key}
	return u.String()
}
