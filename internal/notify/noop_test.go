package notify

import (
	"context"
	"errors"
	"testing"
)

func TestNoop_AlwaysNil(t *testing.T) {
	n := Noop{}
	events := []Event{
		{Kind: EventStart},
		{Kind: EventSuccess, Path: "p", Size: 100},
		{Kind: EventFailure, Err: errors.New("x"), Context: "c"},
	}
	for _, e := range events {
		if err := n.Notify(context.Background(), e); err != nil {
			t.Errorf("Notify(%v) = %v, want nil", e.Kind, err)
		}
	}
}

var _ Notifier = Noop{}
