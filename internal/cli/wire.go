package cli

import (
	"context"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/awsauth"
	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/notify"
	"github.com/cloudscript-technology/dumpscript/internal/storage"
)

// buildStorage wires IRSA credentials (if applicable) into the Storage factory.
// It returns the composed Storage (Logging → Retrying → base adapter).
func buildStorage(ctx context.Context, cfg *config.Config, log *slog.Logger) (storage.Storage, error) {
	var opts storage.Options
	if cfg.Backend == config.BackendS3 {
		creds, err := awsauth.IRSAProvider(ctx, cfg, log)
		if err != nil {
			return nil, err
		}
		opts.AWSCredentials = creds
	}
	return storage.New(ctx, cfg, log, opts)
}

// buildNotifier returns a SlackNotifier when configured, else a Noop.
func buildNotifier(cfg *config.Config, log *slog.Logger) notify.Notifier {
	if cfg.Slack.WebhookURL == "" {
		return notify.Noop{}
	}
	return notify.NewSlack(cfg, log)
}
