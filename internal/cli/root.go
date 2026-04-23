// Package cli wires the cobra command tree. Each subcommand is a Command in
// the Command pattern — self-contained, individually testable.
package cli

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/logging"
)

// NewRoot returns the root cobra command with all subcommands registered.
//
// A bootstrap logger is initialised from LOG_LEVEL / LOG_FORMAT env vars so
// messages emitted before config parsing are still captured; each subcommand
// calls `loggerFromConfig` to replace it once the full Config is available.
func NewRoot() *cobra.Command {
	bootstrap := logging.New(logging.Config{
		Level:  os.Getenv("LOG_LEVEL"),
		Format: os.Getenv("LOG_FORMAT"),
	})

	root := &cobra.Command{
		Use:          "dumpscript",
		Short:        "Database dump and restore tool for S3 and Azure Blob Storage",
		SilenceUsage: true,
		Long: "dumpscript streams pg_dump/mysqldump/mariadb-dump/mongodump output\n" +
			"into a gzipped artifact and uploads it to the configured storage backend.",
	}
	root.PersistentFlags().String("log-level", "", "log level (debug|info|warn|error) — overrides LOG_LEVEL")
	root.PersistentFlags().String("log-format", "", "log format (json|console) — overrides LOG_FORMAT")

	root.AddCommand(
		newDumpCmd(bootstrap),
		newRestoreCmd(bootstrap),
		newCleanupCmd(bootstrap),
	)
	return root
}

// loggerFromConfig returns a new *slog.Logger honouring cfg.LogLevel /
// cfg.LogFormat plus any --log-level / --log-format CLI overrides.
// Adds common structured attrs (subcmd, backend, db_type, periodicity) to
// every subsequent log line.
func loggerFromConfig(cmd *cobra.Command, cfg *config.Config, subcmd string) *slog.Logger {
	levelOverride, _ := cmd.Flags().GetString("log-level")
	formatOverride, _ := cmd.Flags().GetString("log-format")

	level := cfg.LogLevel
	if levelOverride != "" {
		level = levelOverride
	}
	format := cfg.LogFormat
	if formatOverride != "" {
		format = formatOverride
	}

	l := logging.New(logging.Config{Level: level, Format: format})
	return l.With(
		"subcmd", subcmd,
		"db_type", string(cfg.DB.Type),
		"backend", string(cfg.Backend),
		"periodicity", string(cfg.Periodicity),
	)
}
