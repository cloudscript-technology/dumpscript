package cli

import (
	"errors"
	"log/slog"
	"time"

	"github.com/spf13/cobra"

	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/retention"
	"github.com/cloudscript-technology/dumpscript/internal/storage"
)

func newCleanupCmd(log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Delete backup objects older than RETENTION_DAYS under the periodicity prefix",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			log = loggerFromConfig(cmd, cfg, "cleanup")
			log.Info("dumpscript starting",
				"prefix", cfg.Prefix(),
				"retention_days", cfg.RetentionDays)

			if err := cfg.ValidateCommon(); err != nil {
				return err
			}
			if cfg.Periodicity == "" {
				return errors.New("PERIODICITY is required for cleanup")
			}

			store, err := buildStorage(ctx, cfg, log)
			if err != nil {
				return err
			}

			cleaner := retention.New(store, log).WithDryRun(cfg.DryRun)
			if cfg.DryRun {
				log.Info("dry-run mode: deletions will be logged but skipped")
			}
			prefix := storage.PeriodPrefix(cfg)
			_, err = cleaner.Run(ctx, prefix, cfg.RetentionDays, time.Now())
			return err
		},
	}
}
