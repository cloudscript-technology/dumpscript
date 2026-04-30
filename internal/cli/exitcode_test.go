package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/pipeline"
)

func TestExitCode_KnownSentinels(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		want     int
	}{
		{"config", fmt.Errorf("%w: missing DB_HOST", pipeline.ErrConfigInvalid), ExitConfigInvalid},
		{"destination", fmt.Errorf("%w: %v", pipeline.ErrDestinationUnreachable, errors.New("dial tcp: timeout")), ExitDestinationUnreachable},
		{"upload", fmt.Errorf("%w: 503 from S3", pipeline.ErrUploadFailed), ExitUploadFailed},
		{"lock acquire", fmt.Errorf("%w: write failed", pipeline.ErrLockAcquire), ExitLockAcquire},
		{"dump failed", fmt.Errorf("%w: pg_dump exit 1", pipeline.ErrDumpFailed), ExitDumpFailed},
		{"dump truncated", fmt.Errorf("%w: gzip envelope unreadable", pipeline.ErrDumpTruncated), ExitDumpTruncated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(tc.err); got != tc.want {
				t.Errorf("ExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestExitCode_UnknownErrorDefaultsToGeneric(t *testing.T) {
	if got := ExitCode(errors.New("something else")); got != ExitGeneric {
		t.Errorf("got %d, want %d", got, ExitGeneric)
	}
}

func TestExitCode_NilReturnsGeneric(t *testing.T) {
	// Defensive: caller should check err != nil first, but we shouldn't
	// pretend success if they don't.
	if got := ExitCode(nil); got != ExitGeneric {
		t.Errorf("got %d, want %d", got, ExitGeneric)
	}
}

func TestExitCode_WrappedDeeplyStillMatches(t *testing.T) {
	// errors.Is unwraps the full chain — verify behavior across multiple wraps.
	wrapped := fmt.Errorf("outer: %w",
		fmt.Errorf("middle: %w",
			fmt.Errorf("%w: detail", pipeline.ErrDumpFailed)))
	if got := ExitCode(wrapped); got != ExitDumpFailed {
		t.Errorf("deep wrap: got %d, want %d", got, ExitDumpFailed)
	}
}
