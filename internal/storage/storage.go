// Package storage abstracts object storage backends (S3, Azure) behind a single interface.
package storage

import (
	"context"
	"time"
)

// Object is a remote object listing entry.
type Object struct {
	Key      string
	Size     int64
	Modified time.Time
}

// Storage is the port implemented by every backend adapter (Strategy + Adapter patterns).
type Storage interface {
	Upload(ctx context.Context, localPath, key string) error
	UploadBytes(ctx context.Context, data []byte, key string) error
	Download(ctx context.Context, key, localPath string) error
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	DisplayPath(key string) string
}
