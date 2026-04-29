package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/crypt"
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

	key := p.d.Config.S3.Key

	// Dry-run short-circuit: validate config, sourceKey existence, and DB
	// reachability without downloading or applying. Mirrors the dump
	// pipeline's DRY_RUN handling so a freshly applied Restore CR can be
	// smoke-tested before it actually touches the target DB.
	if p.d.Config.DryRun {
		exists, err := p.d.Storage.Exists(ctx, key)
		if err != nil {
			return fmt.Errorf("dry-run: probe sourceKey: %w", err)
		}
		if !exists {
			return fmt.Errorf("dry-run: sourceKey %q not found in storage", key)
		}
		if err := verifier.PostRestore(ctx, p.d.Config, p.d.Log); err != nil {
			return fmt.Errorf("dry-run: target DB unreachable: %w", err)
		}
		p.d.Log.Info("dry-run: validated config + sourceKey + target DB reachability",
			"key", key, "db_host", p.d.Config.DB.Host)
		return nil
	}

	ext := "sql"
	if p.d.Config.DB.Type == config.DBMongo {
		ext = "archive"
	}
	local := filepath.Join(p.d.Config.WorkDir, "dump_restore."+ext+".gz")
	defer func() { _ = os.Remove(local) }()

	// Encrypted artifacts have a `.aes` suffix. Adjust the local download
	// path so the `.aes` is preserved on disk; we'll decrypt in-place
	// before passing the plaintext to the restorer.
	if strings.HasSuffix(key, ".aes") {
		local += ".aes"
	}

	p.d.Log.Info("downloading dump", "key", key, "local", local)
	if err := p.d.Storage.Download(ctx, key, local); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Decrypt if the artifact is AES-encrypted (suffix `.aes`). Sidesteps
	// the restorer entirely — it just sees the original .gz / .zst path.
	restoreSrc := local
	if strings.HasSuffix(local, ".aes") {
		decrypted, err := p.decryptArtifact(local)
		if err != nil {
			return fmt.Errorf("decrypt artifact: %w", err)
		}
		defer func() { _ = os.Remove(decrypted) }()
		restoreSrc = decrypted
	}

	p.d.Log.Info("applying restore",
		"db_type", p.d.Config.DB.Type, "host", p.d.Config.DB.Host, "db_name", p.d.Config.DB.Name)
	if err := p.d.Restorer.Restore(ctx, restoreSrc); err != nil {
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

// decryptArtifact reads the AES-encrypted ciphertext at encPath, decrypts it
// using the configured key file, and writes plaintext to the same path with
// the trailing `.aes` stripped. Returns the plaintext path. The caller is
// responsible for removing it after use.
func (p *Restore) decryptArtifact(encPath string) (string, error) {
	if p.d.Config.EncryptionKeyFile == "" {
		return "", fmt.Errorf("artifact has .aes suffix but ENCRYPTION_KEY_FILE is not set")
	}
	key, err := crypt.LoadKey(p.d.Config.EncryptionKeyFile)
	if err != nil {
		return "", fmt.Errorf("load key: %w", err)
	}
	plainPath := strings.TrimSuffix(encPath, ".aes")
	if err := crypt.DecryptFile(encPath, plainPath, key); err != nil {
		return "", err
	}
	p.d.Log.Info("artifact decrypted (AES-256-GCM)", "src", encPath, "dst", plainPath)
	return plainPath, nil
}
