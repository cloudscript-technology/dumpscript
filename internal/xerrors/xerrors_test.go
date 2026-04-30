package xerrors

import (
	"errors"
	"testing"
)

func TestFirst(t *testing.T) {
	e1 := errors.New("one")
	e2 := errors.New("two")

	tests := []struct {
		name string
		in   []error
		want error
	}{
		{"nil only", []error{nil, nil}, nil},
		{"single", []error{e1}, e1},
		{"first wins", []error{e1, e2}, e1},
		{"skip leading nil", []error{nil, e1, e2}, e1},
		{"empty", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := First(tc.in...)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
