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
	"log/slog"
	"os"
	"path/filepath"
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
	return AcquireWithGrace(ctx, store, key, info, 0, nil)
}

// AcquireWithGrace is like Acquire but, when an existing lock is older than
// `grace` (computed from the lock's recorded StartedAt timestamp), logs a
// warning and overwrites it instead of returning ErrLocked. A previous run
// that crashed without releasing the lock would otherwise jam every future
// scheduled run forever; the grace period is the operator's escape hatch.
//
// Pass grace=0 to keep the strict no-overwrite behavior. Pass log=nil to
// suppress the takeover warning.
func AcquireWithGrace(ctx context.Context, store storage.Storage, key string, info Info, grace time.Duration, log *slog.Logger) error {
	exists, err := store.Exists(ctx, key)
	if err != nil {
		return fmt.Errorf("check lock: %w", err)
	}
	if exists {
		if grace <= 0 {
			return ErrLocked
		}
		stale, prev, err := isStale(ctx, store, key, grace)
		if err != nil {
			// Read failure shouldn't escalate to "lock is fine" — fail closed.
			return fmt.Errorf("read existing lock: %w", err)
		}
		if !stale {
			return ErrLocked
		}
		if log != nil {
			log.Warn("taking over stale lock — previous run did not release it",
				"key", key,
				"previous_execution_id", prev.ExecutionID,
				"previous_started_at", prev.StartedAt,
				"grace", grace)
		}
		// Fall through to overwrite below.
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

// isStale downloads the existing lock JSON and reports whether its StartedAt
// is older than `grace`. Returns (true, prev, nil) on stale, (false, prev, nil)
// on still-fresh, and propagates any read error.
func isStale(ctx context.Context, store storage.Storage, key string, grace time.Duration) (bool, Info, error) {
	tmp, err := os.CreateTemp("", "dumpscript-lock-*.json")
	if err != nil {
		return false, Info{}, fmt.Errorf("temp file: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath) //nolint:errcheck

	if err := store.Download(ctx, key, tmpPath); err != nil {
		return false, Info{}, fmt.Errorf("download lock: %w", err)
	}
	raw, err := os.ReadFile(filepath.Clean(tmpPath))
	if err != nil {
		return false, Info{}, fmt.Errorf("read lock file: %w", err)
	}
	var prev Info
	if err := json.Unmarshal(raw, &prev); err != nil {
		// Lock content is malformed — treat as stale rather than blocking
		// forever on a corrupted JSON.
		return true, prev, nil
	}
	if prev.StartedAt.IsZero() {
		return true, prev, nil
	}
	return time.Since(prev.StartedAt) > grace, prev, nil
}

// Release deletes the lock file.
func Release(ctx context.Context, store storage.Storage, key string) error {
	return store.Delete(ctx, key)
}
