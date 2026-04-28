package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/restorer"
	"github.com/cloudscript-technology/dumpscript/internal/storage"
	"github.com/cloudscript-technology/dumpscript/internal/verifier"
)

// RestoreDeps aggregates dependencies injected into the restore pipeline.
type RestoreDeps struct {
	Config   *config.Config
	Restorer restorer.Restorer
	Storage  storage.Storage
	Log      *slog.Logger
}

// Restore is the Template Method for a restore execution.
type Restore struct{ d RestoreDeps }

func NewRestore(d RestoreDeps) *Restore { return &Restore{d: d} }

// Run downloads the object at S3_KEY, then applies it to the live DB.
func (p *Restore) Run(ctx context.Context) error {
	if err := p.d.Config.ValidateRestore(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := os.MkdirAll(p.d.Config.WorkDir, 0o755); err != nil {
		return fmt.Errorf("mkdir workdir: %w", err)
	}

	ext := "sql"
	if p.d.Config.DB.Type == config.DBMongo {
		ext = "archive"
	}
	local := filepath.Join(p.d.Config.WorkDir, "dump_restore."+ext+".gz")
	defer func() { _ = os.Remove(local) }()

	key := p.d.Config.S3.Key
	p.d.Log.Info("downloading dump", "key", key, "local", local)
	if err := p.d.Storage.Download(ctx, key, local); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	p.d.Log.Info("applying restore",
		"db_type", p.d.Config.DB.Type, "host", p.d.Config.DB.Host, "db_name", p.d.Config.DB.Name)
	if err := p.d.Restorer.Restore(ctx, local); err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	// Post-restore reachability check — confirms the engine is still
	// answering connections after the import. Helps catch the case where
	// restore returned 0 but the import actually corrupted/crashed the
	// engine. Failure here surfaces as a non-zero exit (and Failed phase on
	// the operator side).
	if err := verifier.PostRestore(ctx, p.d.Config, p.d.Log); err != nil {
		return fmt.Errorf("post-restore verify: %w", err)
	}

	p.d.Log.Info("restore completed", "key", key)
	return nil
}
