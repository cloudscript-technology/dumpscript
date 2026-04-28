package cli

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/cloudscript-technology/dumpscript/internal/clock"
	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/dumper"
	"github.com/cloudscript-technology/dumpscript/internal/metrics"
	"github.com/cloudscript-technology/dumpscript/internal/pipeline"
	"github.com/cloudscript-technology/dumpscript/internal/retention"
	"github.com/cloudscript-technology/dumpscript/internal/verifier"
)

func newDumpCmd(log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "dump",
		Short: "Dump the configured database and upload to storage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			log = loggerFromConfig(cmd, cfg, "dump")
			log.Info("dumpscript starting",
				"host", cfg.DB.Host,
				"db_name", cfg.DB.Name,
				"timeout", cfg.DumpTimeout,
				"retention_days", cfg.RetentionDays)

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.DumpTimeout)
			defer cancel()

			d, err := dumper.New(cfg, log)
			if err != nil {
				return err
			}
			// Wrap with retry decorator so transient failures (network blip,
			// brief DB unavailability) get a second chance.
			d = dumper.NewRetrying(d, dumper.RetryConfig{
				MaxAttempts:    cfg.DumpRetries,
				InitialBackoff: cfg.DumpRetryBackoff,
				MaxBackoff:     cfg.DumpRetryMaxBackoff,
			}, log)
			v, err := verifier.New(cfg, log)
			if err != nil {
				return err
			}
			store, err := buildStorage(ctx, cfg, log)
			if err != nil {
				return err
			}
			notifier := buildNotifier(cfg, log)

			var cleaner *retention.Cleaner
			if cfg.RetentionDays > 0 {
				cleaner = retention.New(store, log)
			}

			mx := metrics.New(cfg, log)
			// When METRICS_LISTEN is set and the active metrics impl is the
			// Prometheus one, expose it via HTTP so a scraper can pull it
			// directly. CronJob-style runs typically leave this empty and
			// rely on the operator's metrics endpoint instead.
			if cfg.MetricsListen != "" {
				if p, ok := mx.(*metrics.Prom); ok {
					metrics.ServeMetrics(cfg.MetricsListen, p.Registry(), log)
				} else {
					log.Warn("METRICS_LISTEN set but metrics implementation is not Prometheus; ignoring")
				}
			}

			p := pipeline.NewDump(pipeline.DumpDeps{
				Config:   cfg,
				Dumper:   d,
				Verifier: v,
				Storage:  store,
				Notifier: notifier,
				Metrics:  mx,
				Cleaner:  cleaner,
				Clock:    clock.System{},
				Log:      log,
			})
			return p.Run(ctx)
		},
	}
}
