package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/prometheus/common/expfmt"
)

// Prom is the Prometheus client_golang-backed Metrics implementation.
// Registry is isolated per instance so tests don't leak global state.
type Prom struct {
	reg *prometheus.Registry
	log *slog.Logger

	pushgatewayURL string
	jobName        string
	instance       string
	logOnExit      bool

	runs        *prometheus.CounterVec
	dumpDur     *prometheus.HistogramVec
	dumpSize    *prometheus.HistogramVec
	uploadDur   *prometheus.HistogramVec
	uploadSize  *prometheus.HistogramVec
	retries     *prometheus.CounterVec
	lastSuccess prometheus.Gauge
	startedAt   prometheus.Gauge

	mu      sync.Mutex
	flushed bool
}

// Config carries the knobs read from env (kept here instead of the config
// package so metrics stays self-contained and easy to test).
type Config struct {
	PushgatewayURL string
	JobName        string
	Instance       string
	LogOnExit      bool
}

// NewProm builds a Prometheus Metrics with an isolated registry.
func NewProm(cfg Config, log *slog.Logger) *Prom {
	reg := prometheus.NewRegistry()

	runs := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dumpscript_runs_total",
		Help: "Total number of dumpscript runs, labeled by result.",
	}, []string{"result"})

	dumpDur := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "dumpscript_dump_duration_seconds",
		Help:    "Duration of the database dump phase.",
		Buckets: []float64{1, 5, 15, 30, 60, 180, 600, 1800, 3600},
	}, []string{"engine"})

	dumpSize := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "dumpscript_dump_bytes",
		Help:    "Compressed size of the produced dump artefact.",
		Buckets: prometheus.ExponentialBuckets(1<<20, 4, 10), // 1MiB × 4^n
	}, []string{"engine"})

	uploadDur := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "dumpscript_upload_duration_seconds",
		Help:    "Duration of the upload phase to remote storage.",
		Buckets: []float64{0.5, 1, 5, 15, 60, 180, 600, 1800},
	}, []string{"backend"})

	uploadSize := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "dumpscript_upload_bytes",
		Help:    "Bytes transferred in the upload phase.",
		Buckets: prometheus.ExponentialBuckets(1<<20, 4, 10),
	}, []string{"backend"})

	retries := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "dumpscript_storage_retries_total",
		Help: "Storage operation retry attempts, labeled by op.",
	}, []string{"op"})

	lastSuccess := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dumpscript_last_success_timestamp_seconds",
		Help: "Unix timestamp of the most recent successful run.",
	})

	startedAt := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "dumpscript_run_started_timestamp_seconds",
		Help: "Unix timestamp of this run's start.",
	})
	startedAt.Set(float64(time.Now().Unix()))

	reg.MustRegister(runs, dumpDur, dumpSize, uploadDur, uploadSize,
		retries, lastSuccess, startedAt)

	return &Prom{
		reg:            reg,
		log:            log,
		pushgatewayURL: cfg.PushgatewayURL,
		jobName:        cfg.JobName,
		instance:       cfg.Instance,
		logOnExit:      cfg.LogOnExit,
		runs:           runs,
		dumpDur:        dumpDur,
		dumpSize:       dumpSize,
		uploadDur:      uploadDur,
		uploadSize:     uploadSize,
		retries:        retries,
		lastSuccess:    lastSuccess,
		startedAt:      startedAt,
	}
}

// Registry exposes the underlying Prometheus registry (used by /metrics HTTP
// handlers in long-running processes and by tests).
func (p *Prom) Registry() *prometheus.Registry { return p.reg }

func (p *Prom) RecordRun(r Result) { p.runs.WithLabelValues(string(r)).Inc() }

func (p *Prom) RecordDump(engine string, d time.Duration, bytes int64) {
	p.dumpDur.WithLabelValues(engine).Observe(d.Seconds())
	if bytes > 0 {
		p.dumpSize.WithLabelValues(engine).Observe(float64(bytes))
	}
}

func (p *Prom) RecordUpload(backend string, d time.Duration, bytes int64) {
	p.uploadDur.WithLabelValues(backend).Observe(d.Seconds())
	if bytes > 0 {
		p.uploadSize.WithLabelValues(backend).Observe(float64(bytes))
	}
}

func (p *Prom) RecordRetry(op string) { p.retries.WithLabelValues(op).Inc() }
func (p *Prom) RecordLastSuccess()    { p.lastSuccess.SetToCurrentTime() }

// Flush pushes to the Pushgateway (if configured) and optionally writes the
// metrics text format to stderr. Safe to call multiple times — only the first
// invocation actually pushes/prints.
func (p *Prom) Flush(ctx context.Context) error {
	p.mu.Lock()
	if p.flushed {
		p.mu.Unlock()
		return nil
	}
	p.flushed = true
	p.mu.Unlock()

	var pushErr error
	if p.pushgatewayURL != "" {
		pusher := push.New(p.pushgatewayURL, p.jobName).Gatherer(p.reg)
		if p.instance != "" {
			pusher = pusher.Grouping("instance", p.instance)
		}
		if err := pusher.PushContext(ctx); err != nil {
			p.log.Warn("pushgateway push failed", "url", p.pushgatewayURL, "err", err)
			pushErr = fmt.Errorf("pushgateway: %w", err)
		} else {
			p.log.Info("metrics pushed", "url", p.pushgatewayURL, "job", p.jobName)
		}
	}

	if p.logOnExit {
		mfs, err := p.reg.Gather()
		if err == nil {
			for _, mf := range mfs {
				_, _ = expfmt.MetricFamilyToText(os.Stderr, mf)
			}
		}
	}

	return pushErr
}
