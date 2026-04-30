package notify

import "context"

// Noop is a Notifier that silently drops events. Use when no channel is configured.
type Noop struct{}

func (Noop) Notify(_ context.Context, _ Event) error { return nil }
