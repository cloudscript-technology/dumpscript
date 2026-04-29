package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.BackendAzure, func(ctx context.Context, cfg *config.Config, log *slog.Logger, _ Options) (Storage, error) {
		return NewAzure(ctx, cfg, log)
	})
}

// Azure is the Adapter around azure-sdk-for-go/.../azblob implementing the Storage port.
type Azure struct {
	cfg    *config.Config
	log    *slog.Logger
	client *azblob.Client
}

func NewAzure(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Azure, error) {
	serviceURL := cfg.Azure.Endpoint
	if serviceURL == "" {
		serviceURL = fmt.Sprintf("https://%s.blob.core.windows.net", cfg.Azure.Account)
	}

	var client *azblob.Client
	if cfg.Azure.SASToken != "" {
		c, err := azblob.NewClientWithNoCredential(serviceURL+"?"+cfg.Azure.SASToken, nil)
		if err != nil {
			return nil, fmt.Errorf("azure SAS client: %w", err)
		}
		client = c
	} else {
		cred, err := azblob.NewSharedKeyCredential(cfg.Azure.Account, cfg.Azure.Key)
		if err != nil {
			return nil, fmt.Errorf("azure shared key: %w", err)
		}
		c, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("azure key client: %w", err)
		}
		client = c
	}

	a := &Azure{cfg: cfg, log: log, client: client}

	// Opt-in: create the blob container at startup if missing. Idempotent —
	// 409 ContainerAlreadyExists is treated as success. By default
	// dumpscript expects the container to be pre-provisioned by ops/IaC.
	if cfg.Azure.CreateContainerIfMissing {
		if _, err := client.CreateContainer(ctx, cfg.Azure.Container, nil); err != nil {
			if bloberror.HasCode(err, bloberror.ContainerAlreadyExists) {
				log.Info("azure: container already exists", "container", cfg.Azure.Container)
			} else {
				var respErr *azcore.ResponseError
				if errors.As(err, &respErr) {
					return nil, fmt.Errorf("create container %q: status=%d code=%q msg=%q",
						cfg.Azure.Container, respErr.StatusCode, respErr.ErrorCode, respErr.Error())
				}
				return nil, fmt.Errorf("create container %q: %w", cfg.Azure.Container, err)
			}
		} else {
			log.Info("azure: created blob container", "container", cfg.Azure.Container)
		}
	}

	return a, nil
}

func (a *Azure) Upload(ctx context.Context, localPath, key string) error {
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

	chunk, err := parseSize(a.cfg.Upload.ChunkSize)
	if err != nil {
		return err
	}

	if _, err = a.client.UploadFile(ctx, a.cfg.Azure.Container, key, f, &azblob.UploadFileOptions{
		BlockSize:   chunk,
		Concurrency: uint16(a.cfg.Upload.Concurrency),
		Metadata:    metadataPtrMap(backupTags(a.cfg)),
	}); err != nil {
		return fmt.Errorf("azure upload: %w", err)
	}

	// Post-upload size sanity: catches truncated commits where the SDK
	// returned success but the blob ended up shorter than the local source.
	blobClient := a.client.ServiceClient().
		NewContainerClient(a.cfg.Azure.Container).
		NewBlobClient(key)
	props, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		return fmt.Errorf("azure verify properties: %w", err)
	}
	if props.ContentLength != nil && *props.ContentLength != localSize {
		return fmt.Errorf("azure upload integrity check failed: local=%d remote=%d (key=%s)",
			localSize, *props.ContentLength, key)
	}
	a.log.Debug("azure upload integrity verified", "key", key, "size", localSize)
	return nil
}

func (a *Azure) UploadBytes(ctx context.Context, data []byte, key string) error {
	_, err := a.client.UploadBuffer(ctx, a.cfg.Azure.Container, key, data, nil)
	return err
}

func (a *Azure) Exists(ctx context.Context, key string) (bool, error) {
	blobClient := a.client.ServiceClient().NewContainerClient(a.cfg.Azure.Container).NewBlobClient(key)
	_, err := blobClient.GetProperties(ctx, nil)
	if err == nil {
		return true, nil
	}
	// Two observed shapes for "blob doesn't exist":
	//   - bloberror.BlobNotFound  (named code in body)
	//   - generic 404 with no specific code (Azurite sometimes returns this
	//     for HEAD on a non-existent blob — same pattern as the List fix
	//     in this file)
	// Translate both into (false, nil) so callers (lock.Acquire, etc.) can
	// decide based on a clean boolean instead of swallowing retry-eligible
	// errors all the way up the storage retry decorator.
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return false, nil
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) && respErr.StatusCode == 404 {
		return false, nil
	}
	return false, err
}

func (a *Azure) Download(ctx context.Context, key, localPath string) error {
	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = a.client.DownloadFile(ctx, a.cfg.Azure.Container, key, f, nil)
	return err
}

func (a *Azure) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	pager := a.client.NewListBlobsFlatPager(a.cfg.Azure.Container, &azblob.ListBlobsFlatOptions{
		Prefix: &prefix,
	})
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			// Azurite (latest) returns 404 when listing an EMPTY container
			// even after the container was created via a signed PUT — the
			// emulator's container-state seems to be inconsistent across the
			// internal route used by Azure SDK paginated listing. Real Azure
			// correctly returns 200 + empty EnumerationResults.
			//
			// Two observed shapes:
			//   - bloberror.ContainerNotFound (named code in body)
			//   - generic 404 with no specific code (just StatusCode=404)
			// Treat both as "empty list" so the dump pipeline's reachability
			// preflight (which invokes List) doesn't abort spuriously. If
			// the container is genuinely missing, the later UploadFile call
			// surfaces the error clearly with upload-time context.
			if bloberror.HasCode(err, bloberror.ContainerNotFound) {
				return out, nil
			}
			var respErr *azcore.ResponseError
			if errors.As(err, &respErr) && respErr.StatusCode == 404 {
				return out, nil
			}
			return nil, err
		}
		for _, b := range page.Segment.BlobItems {
			o := Object{}
			if b.Name != nil {
				o.Key = *b.Name
			}
			if b.Properties != nil {
				if b.Properties.ContentLength != nil {
					o.Size = *b.Properties.ContentLength
				}
				if b.Properties.LastModified != nil {
					o.Modified = *b.Properties.LastModified
				}
			}
			out = append(out, o)
		}
	}
	return out, nil
}

func (a *Azure) Delete(ctx context.Context, key string) error {
	_, err := a.client.DeleteBlob(ctx, a.cfg.Azure.Container, key, nil)
	return err
}

func (a *Azure) DisplayPath(key string) string {
	u := url.URL{Scheme: "azure", Host: a.cfg.Azure.Container, Path: "/" + key}
	return u.String()
}
