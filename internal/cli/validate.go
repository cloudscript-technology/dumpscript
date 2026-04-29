package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func newValidateCmd(log *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the loaded configuration and check destination reachability",
		Long: "Loads the env-driven configuration, runs the per-subcommand " +
			"validation rules (database connection requirements, storage backend " +
			"required fields, etc.), and probes the storage backend's List call " +
			"to confirm reachability — without doing any dump or upload.\n\n" +
			"Use this to debug a freshly applied BackupSchedule or to smoke-test " +
			"credentials in a new environment.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintln(os.Stderr, "✗ config load failed:", err)
				return err
			}
			log = loggerFromConfig(cmd, cfg, "validate")

			fmt.Println("dumpscript validate")
			fmt.Println("─────────────────────────────────────────────────────────")
			printSummary(cfg)
			fmt.Println("─────────────────────────────────────────────────────────")

			// Phase 1: ValidateCommon (DB type, storage backend basics).
			if err := cfg.ValidateCommon(); err != nil {
				fmt.Fprintln(os.Stderr, "✗ ValidateCommon failed:", err)
				return err
			}
			fmt.Println("✔ ValidateCommon passed (DB type, storage backend, required keys)")

			// Phase 2: per-engine connection requirements (DB_HOST, DB_USER,
			// engine-specific exceptions for sqlite/redis/etcd/elasticsearch).
			// Without this the validate subcommand would silently green-light
			// configs missing fields the dump pipeline would reject at run
			// time.
			if err := cfg.ValidateConnection(); err != nil {
				fmt.Fprintln(os.Stderr, "✗ ValidateConnection failed:", err)
				return err
			}
			fmt.Println("✔ ValidateConnection passed (per-engine host/user requirements)")

			// Phase 3: storage reachability — uses the same path as the dump
			// pipeline's preflight.
			ctx := cmd.Context()
			if err := probeStorage(ctx, cfg, log); err != nil {
				fmt.Fprintln(os.Stderr, "✗ storage reachability failed:", err)
				return err
			}
			fmt.Println("✔ storage reachable (List under prefix succeeded)")

			fmt.Println()
			fmt.Println("All validations passed — config is ready for `dumpscript dump|restore`.")
			return nil
		},
	}
}

// probeStorage builds the storage backend, then calls List on the configured
// prefix. Mirrors the dump pipeline's preflight reachability check.
func probeStorage(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	store, err := buildStorage(ctx, cfg, log)
	if err != nil {
		return fmt.Errorf("build storage: %w", err)
	}
	if _, err := store.List(ctx, cfg.Prefix()); err != nil {
		return fmt.Errorf("list under prefix %q: %w", cfg.Prefix(), err)
	}
	return nil
}

// printSummary prints the config's effective values (with secrets redacted)
// so operators see exactly what envconfig parsed without dumping the env.
func printSummary(cfg *config.Config) {
	fmt.Println("  database:")
	fmt.Printf("    type           : %s\n", cfg.DB.Type)
	fmt.Printf("    host           : %s\n", cfg.DB.Host)
	fmt.Printf("    port           : %d\n", cfg.DB.Port)
	fmt.Printf("    name           : %s\n", cfg.DB.Name)
	fmt.Printf("    user           : %s\n", cfg.DB.User)
	fmt.Printf("    password       : %s\n", maskNonEmpty(cfg.DB.Password))
	if cfg.DB.DumpOptions != "" {
		fmt.Printf("    dump_options   : %s\n", cfg.DB.DumpOptions)
	}

	fmt.Println("  storage:")
	fmt.Printf("    backend        : %s\n", cfg.Backend)
	switch cfg.Backend {
	case config.BackendS3:
		fmt.Printf("    bucket         : %s\n", cfg.S3.Bucket)
		fmt.Printf("    prefix         : %s\n", cfg.S3.Prefix)
		fmt.Printf("    region         : %s\n", cfg.S3.Region)
		if cfg.S3.EndpointURL != "" {
			fmt.Printf("    endpoint       : %s\n", cfg.S3.EndpointURL)
		}
		if cfg.S3.RoleARN != "" {
			fmt.Printf("    role_arn       : %s (IRSA)\n", cfg.S3.RoleARN)
		} else {
			fmt.Printf("    access_key     : %s\n", maskNonEmpty(cfg.S3.AccessKeyID))
			fmt.Printf("    secret_key     : %s\n", maskNonEmpty(cfg.S3.SecretAccessKey))
		}
		if cfg.S3.SSE != "" {
			fmt.Printf("    sse            : %s\n", cfg.S3.SSE)
		}
	case config.BackendAzure:
		fmt.Printf("    account        : %s\n", cfg.Azure.Account)
		fmt.Printf("    container      : %s\n", cfg.Azure.Container)
		fmt.Printf("    prefix         : %s\n", cfg.Azure.Prefix)
		if cfg.Azure.Endpoint != "" {
			fmt.Printf("    endpoint       : %s\n", cfg.Azure.Endpoint)
		}
		fmt.Printf("    key            : %s\n", maskNonEmpty(cfg.Azure.Key))
	case config.BackendGCS:
		fmt.Printf("    bucket         : %s\n", cfg.GCS.Bucket)
		fmt.Printf("    prefix         : %s\n", cfg.GCS.Prefix)
		fmt.Printf("    project_id     : %s\n", cfg.GCS.ProjectID)
		if cfg.GCS.Endpoint != "" {
			fmt.Printf("    endpoint       : %s\n", cfg.GCS.Endpoint)
		}
	}

	fmt.Println("  pipeline:")
	fmt.Printf("    periodicity    : %s\n", cfg.Periodicity)
	fmt.Printf("    retention_days : %d\n", cfg.RetentionDays)
	fmt.Printf("    dry_run        : %t\n", cfg.DryRun)
	fmt.Printf("    compression    : %s\n", cfg.CompressionType)
	fmt.Printf("    dump_timeout   : %s\n", cfg.DumpTimeout)
	fmt.Printf("    log_level      : %s\n", cfg.LogLevel)
	fmt.Printf("    log_format     : %s\n", cfg.LogFormat)
}

// maskNonEmpty returns a fixed-width "[REDACTED]" string when v is non-empty,
// or "(empty)" otherwise. Avoids accidentally revealing the length of secrets.
func maskNonEmpty(v string) string {
	if v == "" {
		return "(empty)"
	}
	return "[REDACTED]"
}
