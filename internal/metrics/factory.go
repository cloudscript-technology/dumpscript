package metrics

import (
	"log/slog"
	"os"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// New returns the configured Metrics implementation. When
// cfg.Prometheus.Enabled is false (default) it returns a zero-cost Noop.
func New(cfg *config.Config, log *slog.Logger) Metrics {
	if !cfg.Prometheus.Enabled {
		return Noop{}
	}
	instance := cfg.Prometheus.Instance
	if instance == "" {
		instance, _ = os.Hostname()
	}
	jobName := cfg.Prometheus.JobName
	if jobName == "" {
		jobName = "dumpscript"
	}
	return NewProm(Config{
		PushgatewayURL: cfg.Prometheus.PushgatewayURL,
		JobName:        jobName,
		Instance:       instance,
		LogOnExit:      cfg.Prometheus.LogOnExit,
	}, log)
}
