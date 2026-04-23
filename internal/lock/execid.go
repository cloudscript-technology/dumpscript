package lock

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewExecutionID returns a short random hex identifier (16 hex chars / 64 bits)
// used to distinguish individual dumpscript runs in logs, Slack notifications,
// and lock-file contents.
func NewExecutionID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("execution id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
