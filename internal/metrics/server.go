package metrics

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ServeMetrics starts a goroutine serving /metrics on addr (e.g. ":9090") using
// reg as the source. Also registers /healthz and /readyz on the same listener
// so a single port covers Prometheus + Kubernetes liveness/readiness probes.
//
// CronJob-style invocations should leave addr empty; the binary will exit
// before any scraper or probe sees it. The goroutine outlives the function
// call and dies with the process.
func ServeMetrics(addr string, reg *prometheus.Registry, log *slog.Logger) {
	if addr == "" {
		return
	}
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
		mux.HandleFunc("/healthz", livenessHandler)
		mux.HandleFunc("/readyz", readinessHandler)
		srv := &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		if log != nil {
			log.Info("metrics endpoint listening",
				"addr", addr, "paths", "/metrics,/healthz,/readyz")
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) && log != nil {
			log.Warn("metrics server error", "addr", addr, "err", err)
		}
	}()
}

// livenessHandler always returns 200 — once the process is running, it's alive.
// Kubernetes liveness probes restart the pod when this fails, so we want it to
// fail only on truly unrecoverable states (process hung, deadlocked). For a
// short-lived backup binary, hard-coding 200 is correct.
func livenessHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// readinessHandler returns 200 unless the process is shutting down. Kubernetes
// readiness probes pull traffic from the pod when this fails. Same reasoning
// as liveness for a CronJob-style workload — if the binary is up, it's ready.
// Daemon-mode users that need finer readiness gating should swap this for
// their own handler.
func readinessHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}
