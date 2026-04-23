// Package lock provides a best-effort distributed mutex backed by an object
// in Storage. It is used to serialize concurrent dumpscript runs on the same
// day folder.
//
// Semantics:
//   - Acquire: if the lock key exists, return ErrLocked; otherwise write it.
//     A small TOCTOU window exists between Exists and UploadBytes; acceptable
//     for cron-style usage (collisions are rare and the backup is idempotent).
//   - Release: delete the lock key. Safe to call even if the lock is missing.
package lock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/storage"
)

// ErrLocked signals that the destination is currently locked by another run.
var ErrLocked = errors.New("destination is locked by another run")

// Info is the JSON payload stored inside the lock file for forensics.
type Info struct {
	ExecutionID string    `json:"execution_id"`
	Hostname    string    `json:"hostname"`
	StartedAt   time.Time `json:"started_at"`
	PID         int       `json:"pid"`
}

// NewInfo builds a populated Info for the current process.
func NewInfo(execID string) Info {
	host, _ := os.Hostname()
	return Info{
		ExecutionID: execID,
		Hostname:    host,
		StartedAt:   time.Now().UTC(),
		PID:         os.Getpid(),
	}
}

// Acquire writes the lock file at key. Returns ErrLocked if a lock is already present.
func Acquire(ctx context.Context, store storage.Storage, key string, info Info) error {
	exists, err := store.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("check lock: %w", err)
	}
	if exists {
		return ErrLocked
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lock: %w", err)
	}
	if err := store.UploadBytes(ctx, data, key); err != nil {
		return fmt.Errorf("write lock: %w", err)
	}
	return nil
}

// Release deletes the lock file.
func Release(ctx context.Context, store storage.Storage, key string) error {
	return store.Delete(ctx, key)
}
