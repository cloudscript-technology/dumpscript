package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register("stdout", func(cfg *config.Config, log *slog.Logger) (Notifier, bool) {
		if !cfg.NotifyStdout.Enabled {
			return nil, false
		}
		return NewStdout(cfg, log), true
	})
}

// Stdout writes each Event as a single JSON line to os.Stdout. Useful for
// log-based downstream tools (CI dashboards, fluent-bit, etc.) and as the
// simplest possible reference notifier when debugging the registry.
type Stdout struct {
	cfg *config.Config
	log *slog.Logger
	w   io.Writer // overridable in tests
}

func NewStdout(cfg *config.Config, log *slog.Logger) *Stdout {
	return &Stdout{cfg: cfg, log: log, w: os.Stdout}
}

type stdoutPayload struct {
	Event       string `json:"event"`
	Context     string `json:"context,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
	Path        string `json:"path,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Err         string `json:"err,omitempty"`
}

func (s *Stdout) Notify(_ context.Context, e Event) error {
	if !s.cfg.NotifyStdout.Enabled {
		return nil
	}
	if e.Kind == EventSuccess && !s.cfg.NotifyStdout.NotifySuccess {
		return nil
	}
	p := stdoutPayload{
		Event:       string(e.Kind),
		Context:     e.Context,
		ExecutionID: e.ExecutionID,
		Path:        e.Path,
		Size:        e.Size,
	}
	if e.Err != nil {
		p.Err = e.Err.Error()
	}
	buf, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(s.w, string(buf)); err != nil {
		return err
	}
	return nil
}
