package notify

import (
	"context"
	"errors"
	"log/slog"
)

// Multi fans an Event out to every child Notifier. One child failing does
// not prevent the others from receiving the event — all errors are joined
// via errors.Join and surfaced together.
type Multi struct {
	children []Notifier
	log      *slog.Logger
}

// NewMulti is exported for tests / direct composition.
func NewMulti(log *slog.Logger, children ...Notifier) *Multi {
	return &Multi{children: children, log: log}
}

func (m *Multi) Notify(ctx context.Context, e Event) error {
	if len(m.children) == 0 {
		return nil
	}
	errs := make([]error, 0, len(m.children))
	for _, c := range m.children {
		if err := c.Notify(ctx, e); err != nil {
			m.log.Warn("notifier child failed", "err", err, "kind", string(e.Kind))
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
