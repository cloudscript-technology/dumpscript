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
// reg as the source. Useful for daemon-mode deployments that want Prometheus
// to scrape directly instead of pushing through Pushgateway. CronJob-style
// invocations should leave addr empty; the binary will exit before any
// scraper sees it.
//
// The goroutine outlives the function call and dies with the process. There
// is no shutdown call — backups are short-lived; long-running daemons
// shouldn't worry about gracefully stopping a metrics endpoint.
func ServeMetrics(addr string, reg *prometheus.Registry, log *slog.Logger) {
	if addr == "" {
		return
	}
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
		srv := &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		if log != nil {
			log.Info("metrics endpoint listening", "addr", addr, "path", "/metrics")
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) && log != nil {
			log.Warn("metrics server error", "addr", addr, "err", err)
		}
	}()
}
