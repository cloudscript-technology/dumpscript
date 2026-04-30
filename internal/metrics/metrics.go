// Package metrics defines the instrumentation interface used across the pipeline.
// Two implementations are provided:
//
//   - Noop — zero-cost, used when metrics are disabled.
//   - Prom — Prometheus client_golang-backed, with optional Pushgateway flush.
//
// Callers depend on the Metrics interface only; concrete wiring happens in
// internal/cli.
package metrics

import (
	"context"
	"time"
)

// Result labels every run outcome (used as a counter label).
type Result string

const (
	ResultSuccess Result = "success"
	ResultFailure Result = "failure"
	ResultSkipped Result = "skipped"
)

// Metrics is the instrumentation port consumed by the pipeline.
// Implementations MUST be safe to call concurrently.
type Metrics interface {
	RecordRun(result Result)
	RecordDump(engine string, duration time.Duration, bytes int64)
	RecordUpload(backend string, duration time.Duration, bytes int64)
	RecordRetry(op string)
	RecordLastSuccess()
	// Flush writes metrics out (Pushgateway and/or stderr). Safe to call
	// multiple times; idempotent.
	Flush(ctx context.Context) error
}

// Noop is a zero-cost Metrics used when Prometheus is disabled.
type Noop struct{}

func (Noop) RecordRun(Result)                          {}
func (Noop) RecordDump(string, time.Duration, int64)   {}
func (Noop) RecordUpload(string, time.Duration, int64) {}
func (Noop) RecordRetry(string)                        {}
func (Noop) RecordLastSuccess()                        {}
func (Noop) Flush(context.Context) error               { return nil }
