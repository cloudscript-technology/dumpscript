// Package restorer defines the Restorer Strategy interface.
package restorer

import "context"

// Restorer applies a downloaded dump to a live database.
type Restorer interface {
	// Restore reads the gzipped dump at gzPath and applies it.
	Restore(ctx context.Context, gzPath string) error
}
