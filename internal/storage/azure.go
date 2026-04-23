package storage

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"

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

func NewAzure(_ context.Context, cfg *config.Config, log *slog.Logger) (*Azure, error) {
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

	return &Azure{cfg: cfg, log: log, client: client}, nil
}

func (a *Azure) Upload(ctx context.Context, localPath, key string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	chunk, err := parseSize(a.cfg.Upload.ChunkSize)
	if err != nil {
		return err
	}

	_, err = a.client.UploadFile(ctx, a.cfg.Azure.Container, key, f, &azblob.UploadFileOptions{
		BlockSize:   chunk,
		Concurrency: uint16(a.cfg.Upload.Concurrency),
	})
	return err
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
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
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
