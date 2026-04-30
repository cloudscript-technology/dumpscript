package cli

import (
	"errors"

	"github.com/cloudscript-technology/dumpscript/internal/pipeline"
)

// Process exit codes. Operators relying on exit codes for alerting (e.g.
// Kubernetes Job conditions, monit, systemd OnFailure=) can branch on these
// instead of parsing log output.
//
// 0 is reserved for success and is never returned by ExitCode (which only
// runs on a non-nil error). The mapping is best-effort: any error that
// doesn't match a known sentinel returns ExitGeneric (1) — the existing
// catch-all behavior.
const (
	ExitSuccess              = 0  // unused here; reserved for clarity
	ExitGeneric              = 1  // default for unclassified errors
	ExitConfigInvalid        = 2  // ErrConfigInvalid
	ExitDestinationUnreachable = 3  // ErrDestinationUnreachable
	ExitUploadFailed         = 5  // ErrUploadFailed
	ExitLockAcquire          = 6  // ErrLockAcquire (write/read failure, NOT contention)
	ExitDumpFailed           = 7  // ErrDumpFailed
	ExitDumpTruncated        = 8  // ErrDumpTruncated
)

// ExitCode maps a pipeline error to a process exit code. Designed to be
// called once at the top of main.go: `os.Exit(cli.ExitCode(err))`.
//
// Returns ExitGeneric (1) for nil so callers don't accidentally exit 0
// on an error path; callers MUST check err != nil before invoking.
func ExitCode(err error) int {
	if err == nil {
		// Defensive — callers shouldn't pass nil, but if they do,
		// don't pretend to be successful.
		return ExitGeneric
	}
	switch {
	case errors.Is(err, pipeline.ErrConfigInvalid):
		return ExitConfigInvalid
	case errors.Is(err, pipeline.ErrDestinationUnreachable):
		return ExitDestinationUnreachable
	case errors.Is(err, pipeline.ErrUploadFailed):
		return ExitUploadFailed
	case errors.Is(err, pipeline.ErrLockAcquire):
		return ExitLockAcquire
	case errors.Is(err, pipeline.ErrDumpFailed):
		return ExitDumpFailed
	case errors.Is(err, pipeline.ErrDumpTruncated):
		return ExitDumpTruncated
	}
	return ExitGeneric
}
