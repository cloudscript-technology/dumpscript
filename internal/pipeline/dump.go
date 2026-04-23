// Package pipeline orchestrates dump and restore workflows using the Template
// Method pattern — a single Run() defines the sequence and delegates each step
// to an injected Strategy (Dumper, Storage, Notifier, Cleaner).
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/clock"
	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/dumper"
	"github.com/cloudscript-technology/dumpscript/internal/lock"
	"github.com/cloudscript-technology/dumpscript/internal/metrics"
	"github.com/cloudscript-technology/dumpscript/internal/notify"
	"github.com/cloudscript-technology/dumpscript/internal/retention"
	"github.com/cloudscript-technology/dumpscript/internal/storage"
	"github.com/cloudscript-technology/dumpscript/internal/verifier"
)

// DumpDeps aggregates dependencies injected into the dump pipeline.
type DumpDeps struct {
	Config   *config.Config
	Dumper   dumper.Dumper
	Verifier verifier.Verifier  // content verification post-dump
	Storage  storage.Storage
	Notifier notify.Notifier
	Metrics  metrics.Metrics    // nil → Noop
	Cleaner  *retention.Cleaner // nil disables retention
	Clock    clock.Clock
	Log      *slog.Logger

	// NewExecutionID is injected for testability; nil means use lock.NewExecutionID.
	NewExecutionID func() (string, error)
}

// Dump is the Template Method for a dump execution.
type Dump struct{ d DumpDeps }

func NewDump(d DumpDeps) *Dump {
	if d.NewExecutionID == nil {
		d.NewExecutionID = lock.NewExecutionID
	}
	if d.Verifier == nil {
		d.Verifier = verifier.Noop{}
	}
	if d.Metrics == nil {
		d.Metrics = metrics.Noop{}
	}
	return &Dump{d: d}
}

// Run is the Template Method — sequences phases and handles cross-cutting
// concerns (metrics flush, failure notification, lock release).
func (p *Dump) Run(ctx context.Context) (retErr error) {
	execID, err := p.d.NewExecutionID()
	if err != nil {
		return fmt.Errorf("execution id: %w", err)
	}
	log := p.d.Log.With("execution_id", execID)
	started := time.Now()
	log.Info("┌─ dump pipeline starting")

	_ = p.d.Notifier.Notify(ctx, notify.Event{Kind: notify.EventStart, ExecutionID: execID})

	// Always flush metrics on exit — independent of retErr semantics.
	defer func() {
		if err := p.d.Metrics.Flush(context.Background()); err != nil {
			log.Warn("metrics flush failed", "err", err)
		}
	}()
	// Always surface failures to the notifier + log a final summary line.
	defer func() {
		elapsed := time.Since(started)
		if retErr != nil {
			p.d.Metrics.RecordRun(metrics.ResultFailure)
			log.Error("└─ dump pipeline failed", "elapsed", elapsed, "err", retErr)
			_ = p.d.Notifier.Notify(ctx, notify.Event{
				Kind:        notify.EventFailure,
				Err:         retErr,
				Context:     "dump pipeline",
				ExecutionID: execID,
			})
		} else {
			log.Info("└─ dump pipeline finished", "elapsed", elapsed)
		}
	}()

	if err := p.d.Config.ValidateDump(); err != nil {
		return fmt.Errorf("%w: %w", ErrConfigInvalid, err)
	}

	if err := p.preflight(ctx, log); err != nil {
		return err
	}

	now := p.d.Clock.Now()
	lockKey := storage.LockKey(p.d.Config, now)

	skipped, err := p.acquireLock(ctx, log, lockKey, execID)
	if err != nil {
		return err
	}
	if skipped {
		return nil // EventSkipped already emitted; not a failure
	}
	defer func() {
		if err := lock.Release(context.Background(), p.d.Storage, lockKey); err != nil {
			log.Warn("lock release failed", "lock", lockKey, "err", err)
		}
	}()

	p.runRetention(ctx, log, now)

	art, err := p.dumpAndVerify(ctx, log)
	if err != nil {
		return err
	}
	defer func() { _ = art.Cleanup() }()

	displayPath, err := p.uploadArtifact(ctx, log, art, now)
	if err != nil {
		return err
	}

	p.d.Metrics.RecordRun(metrics.ResultSuccess)
	p.d.Metrics.RecordLastSuccess()
	_ = p.d.Notifier.Notify(ctx, notify.Event{
		Kind:        notify.EventSuccess,
		Path:        displayPath,
		Size:        art.Size,
		ExecutionID: execID,
	})
	return nil
}

// preflight verifies the destination is reachable before doing any work.
func (p *Dump) preflight(ctx context.Context, log *slog.Logger) error {
	start := time.Now()
	log.Info("├─ [1/4] preflight: verifying destination is reachable",
		"prefix", p.d.Config.Prefix())
	if _, err := p.d.Storage.List(ctx, p.d.Config.Prefix()); err != nil {
		return fmt.Errorf("%w: %w", ErrDestinationUnreachable, err)
	}
	log.Info("│   ✔ destination reachable", "elapsed", time.Since(start))
	return nil
}

// acquireLock tries to write the day-level .lock file. Returns (skipped=true)
// when another run already holds it — callers must treat that as a successful
// no-op (exit 0, EventSkipped).
func (p *Dump) acquireLock(ctx context.Context, log *slog.Logger, lockKey, execID string) (bool, error) {
	log.Info("├─ [2/4] acquire lock", "key", lockKey)
	if err := lock.Acquire(ctx, p.d.Storage, lockKey, lock.NewInfo(execID)); err != nil {
		if errors.Is(err, lock.ErrLocked) {
			display := p.d.Storage.DisplayPath(lockKey)
			log.Warn("│   ⚠ another backup in progress; skipping this run", "lock", display)
			p.d.Metrics.RecordRun(metrics.ResultSkipped)
			_ = p.d.Notifier.Notify(ctx, notify.Event{
				Kind:        notify.EventSkipped,
				ExecutionID: execID,
				Context:     "Lock already held at " + display,
			})
			return true, nil
		}
		return false, fmt.Errorf("%w: %w", ErrLockAcquire, err)
	}
	log.Info("│   ✔ lock acquired")
	return false, nil
}

// runRetention performs best-effort cleanup of old backups. Failures are
// logged and swallowed — the dump itself shouldn't fail because retention
// couldn't list the bucket.
func (p *Dump) runRetention(ctx context.Context, log *slog.Logger, now time.Time) {
	if p.d.Cleaner == nil || p.d.Config.RetentionDays <= 0 {
		return
	}
	prefix := storage.PeriodPrefix(p.d.Config)
	if _, err := p.d.Cleaner.Run(ctx, prefix, p.d.Config.RetentionDays, now); err != nil {
		log.Warn("retention cleanup failed; continuing with dump", "err", err)
	}
}

// dumpAndVerify runs the engine-specific Dumper + both verifiers (gzip
// envelope and per-engine content). Emits metrics for dump duration/size.
func (p *Dump) dumpAndVerify(ctx context.Context, log *slog.Logger) (*dumper.Artifact, error) {
	log.Info("├─ [3/4] dump + verify", "host", p.d.Config.DB.Host, "db_name", p.d.Config.DB.Name)
	dumpStart := time.Now()
	art, err := p.d.Dumper.Dump(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDumpFailed, err)
	}
	p.d.Metrics.RecordDump(string(p.d.Config.DB.Type), time.Since(dumpStart), art.Size)
	log.Info("│   ✔ dump produced", "size", art.Size, "elapsed", time.Since(dumpStart))

	envelopeStart := time.Now()
	if err := art.Verify(); err != nil {
		return art, fmt.Errorf("%w: %w", ErrDumpTruncated, err)
	}
	log.Info("│   ✔ gzip envelope valid", "elapsed", time.Since(envelopeStart))

	contentStart := time.Now()
	if err := p.d.Verifier.Verify(ctx, art.Path); err != nil {
		return art, fmt.Errorf("%w: %w", ErrDumpTruncated, err)
	}
	log.Info("│   ✔ content verified (per-engine)", "elapsed", time.Since(contentStart))
	return art, nil
}

// uploadArtifact pushes the dump to storage and returns its display path.
func (p *Dump) uploadArtifact(ctx context.Context, log *slog.Logger, art *dumper.Artifact, now time.Time) (string, error) {
	filename := filepath.Base(art.Path)
	key := storage.BuildKey(p.d.Config, now, filename)
	log.Info("├─ [4/4] upload to storage", "key", key, "size", art.Size)
	uploadStart := time.Now()
	if err := p.d.Storage.Upload(ctx, art.Path, key); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUploadFailed, err)
	}
	p.d.Metrics.RecordUpload(string(p.d.Config.Backend), time.Since(uploadStart), art.Size)
	displayPath := p.d.Storage.DisplayPath(key)
	log.Info("│   ✔ upload complete", "path", displayPath, "elapsed", time.Since(uploadStart))
	return displayPath, nil
}
