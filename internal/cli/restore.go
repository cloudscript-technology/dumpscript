package cli

import (
	"context"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/pipeline"
	"github.com/cloudscript-technology/dumpscript/internal/restorer"
)

func newRestoreCmd(log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "restore",
		Short: "Download a dump from storage and apply it to the configured database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			log = loggerFromConfig(cmd, cfg, "restore")
			log.Info("dumpscript starting",
				"host", cfg.DB.Host,
				"db_name", cfg.DB.Name,
				"s3_key", cfg.S3.Key,
				"timeout", cfg.RestoreTimeout)

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.RestoreTimeout)
			defer cancel()

			r, err := restorer.New(cfg, log)
			if err != nil {
				return err
			}
			store, err := buildStorage(ctx, cfg, log)
			if err != nil {
				return err
			}

			p := pipeline.NewRestore(pipeline.RestoreDeps{
				Config:   cfg,
				Restorer: r,
				Storage:  store,
				Log:      log,
			})
			return p.Run(ctx)
		},
	}
}
