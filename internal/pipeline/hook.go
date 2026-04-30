package pipeline

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/dumper"
)

// runPostDumpHook executes Config.PostDumpHook with the run's metadata
// exposed as DUMPSCRIPT_* environment variables. Best-effort: timeout via
// the configured PostDumpHookTimeout; failures are logged but never bubble
// up to the pipeline (the dump itself is already safely uploaded).
//
// Variables passed:
//   DUMPSCRIPT_EXECUTION_ID
//   DUMPSCRIPT_ENGINE
//   DUMPSCRIPT_DB_NAME
//   DUMPSCRIPT_DB_HOST
//   DUMPSCRIPT_KEY            (storage key of the dump)
//   DUMPSCRIPT_SIZE_BYTES
//   DUMPSCRIPT_CHECKSUM       (hex sha256, if available)
//   DUMPSCRIPT_DURATION_SECS  (rounded float)
//   DUMPSCRIPT_DISPLAY_PATH   (s3://… / azure://… / gs://…)
func (p *Dump) runPostDumpHook(
	ctx context.Context,
	log *slog.Logger,
	art *dumper.Artifact,
	dumpKey, displayPath, execID string,
	started time.Time,
) {
	hook := p.d.Config.PostDumpHook
	if hook == "" {
		return
	}
	timeout := p.d.Config.PostDumpHookTimeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(hctx, "sh", "-c", hook)
	cmd.Stdout = os.Stderr // keep hook output visible in pod logs
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"DUMPSCRIPT_EXECUTION_ID="+execID,
		"DUMPSCRIPT_ENGINE="+string(p.d.Config.DB.Type),
		"DUMPSCRIPT_DB_NAME="+p.d.Config.DB.Name,
		"DUMPSCRIPT_DB_HOST="+p.d.Config.DB.Host,
		"DUMPSCRIPT_KEY="+dumpKey,
		"DUMPSCRIPT_DISPLAY_PATH="+displayPath,
		"DUMPSCRIPT_SIZE_BYTES="+strconv.FormatInt(art.Size, 10),
		"DUMPSCRIPT_CHECKSUM="+art.Checksum,
		"DUMPSCRIPT_DURATION_SECS="+strconv.FormatFloat(time.Since(started).Seconds(), 'f', 2, 64),
	)

	log.Info("├─ [hook] running post-dump hook", "timeout", timeout)
	if err := cmd.Run(); err != nil {
		log.Warn("post-dump hook failed (dump itself is safe)",
			"hook", hook, "err", err)
		return
	}
	log.Info("│   ✔ post-dump hook completed", "hook", hook)
}
