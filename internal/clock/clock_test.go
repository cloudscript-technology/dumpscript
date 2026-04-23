package clock

import (
	"testing"
	"time"
)

func TestSystem_Now(t *testing.T) {
	before := time.Now()
	got := System{}.Now()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Errorf("System.Now() = %v, expected between %v and %v", got, before, after)
	}
}

func TestFixed_Now(t *testing.T) {
	want := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	got := Fixed{T: want}.Now()
	if !got.Equal(want) {
		t.Errorf("Fixed.Now() = %v, want %v", got, want)
	}
}

// Compile-time assertion that both types satisfy Clock.
var _ Clock = System{}
var _ Clock = Fixed{}
