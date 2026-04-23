// Package clock provides an injectable time source.
package clock

import "time"

// Clock abstracts time.Now so it can be replaced in tests.
type Clock interface {
	Now() time.Time
}

// System is the production clock.
type System struct{}

func (System) Now() time.Time { return time.Now() }

// Fixed is a deterministic clock for tests.
type Fixed struct{ T time.Time }

func (f Fixed) Now() time.Time { return f.T }
