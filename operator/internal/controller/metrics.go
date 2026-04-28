/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
*/

package controller

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// Custom Prometheus metrics for the operator. Registered with the
// controller-runtime registry so they're served on the same /metrics endpoint
// the manager already exposes (no extra HTTP server, no extra port).
//
// Labels follow Prometheus best practices:
//   - low cardinality: namespace + schedule (or restore) name + engine + result
//   - "result" is a closed set: success | failure
//
// Metrics are incremented from the reconcilers when a Job's terminal state is
// observed for the first time (i.e., when LastSuccessTime / LastFailureTime
// moves forward). Re-reconciles of the same Job do not double-count.
var (
	BackupTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dumpscript_backup_total",
			Help: "Number of completed backup runs by terminal status.",
		},
		[]string{"namespace", "schedule", "engine", "result"},
	)

	BackupDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dumpscript_backup_duration_seconds",
			Help:    "Duration of backup runs from Job start to completion.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 14), // 1s..~4h
		},
		[]string{"namespace", "schedule", "engine", "result"},
	)

	RestoreTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dumpscript_restore_total",
			Help: "Number of completed restore runs by terminal status.",
		},
		[]string{"namespace", "restore", "engine", "result"},
	)

	RestoreDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dumpscript_restore_duration_seconds",
			Help:    "Duration of restore runs from Job start to completion.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 14),
		},
		[]string{"namespace", "restore", "engine", "result"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		BackupTotal,
		BackupDurationSeconds,
		RestoreTotal,
		RestoreDurationSeconds,
	)
}
