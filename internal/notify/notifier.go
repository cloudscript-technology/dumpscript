// Package notify defines the Notifier Observer interface and its events.
package notify

import "context"

// EventKind describes what happened in the pipeline.
type EventKind string

const (
	EventStart   EventKind = "start"
	EventSuccess EventKind = "success"
	EventFailure EventKind = "failure"
	EventSkipped EventKind = "skipped"
)

// Event is dispatched by the pipeline to every subscribed Notifier.
type Event struct {
	Kind        EventKind
	Path        string
	Size        int64
	Err         error
	Context     string
	ExecutionID string
}

// Notifier is the Observer contract.
type Notifier interface {
	Notify(ctx context.Context, e Event) error
}
