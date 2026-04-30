package pipeline

import (
	"errors"

	"github.com/cloudscript-technology/dumpscript/internal/lock"
)

// Sentinel errors emitted by the pipeline. Callers can use `errors.Is` to
// branch on these without parsing error messages.
//
// All sentinels are wrapped by concrete errors via `fmt.Errorf("%w: …", …)`,
// preserving both the category and the underlying cause.
var (
	// ErrConfigInvalid indicates the configuration failed Validate*.
	ErrConfigInvalid = errors.New("invalid configuration")

	// ErrDestinationUnreachable indicates the storage preflight List failed.
	ErrDestinationUnreachable = errors.New("destination unreachable")

	// ErrLockHeld is an alias for lock.ErrLocked, exported at the pipeline
	// boundary so callers don't need to import the lock package.
	ErrLockHeld = lock.ErrLocked

	// ErrLockAcquire indicates the lock file could not be written (not a contention).
	ErrLockAcquire = errors.New("acquire lock failed")

	// ErrDumpFailed indicates the underlying dump command returned non-zero.
	ErrDumpFailed = errors.New("dump failed")

	// ErrDumpTruncated indicates the gzip envelope OR per-engine content
	// verification rejected the dump (truncated / corrupt).
	ErrDumpTruncated = errors.New("dump is truncated or invalid")

	// ErrUploadFailed indicates the upload to storage failed after retries.
	ErrUploadFailed = errors.New("upload failed")
)
