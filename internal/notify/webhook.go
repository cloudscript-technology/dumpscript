package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register("webhook", func(cfg *config.Config, log *slog.Logger) (Notifier, bool) {
		if cfg.Webhook.URL == "" {
			return nil, false
		}
		return NewWebhook(cfg, log), true
	})
}

// Webhook is a Notifier that POSTs the Event itself as JSON to any HTTP
// receiver. Use this to integrate with PagerDuty Events API, Opsgenie,
// custom internal endpoints, n8n, Zapier, etc.
type Webhook struct {
	cfg  *config.Config
	log  *slog.Logger
	http *http.Client
}

func NewWebhook(cfg *config.Config, log *slog.Logger) *Webhook {
	return &Webhook{
		cfg:  cfg,
		log:  log,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// webhookPayload is the JSON shape POSTed to cfg.Webhook.URL.
type webhookPayload struct {
	Kind        string `json:"kind"`
	Context     string `json:"context,omitempty"`
	ExecutionID string `json:"execution_id,omitempty"`
	Path        string `json:"path,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Err         string `json:"err,omitempty"`
}

func (w *Webhook) Notify(ctx context.Context, e Event) error {
	if w.cfg.Webhook.URL == "" {
		return nil
	}
	if e.Kind == EventSuccess && !w.cfg.Webhook.NotifySuccess {
		return nil
	}
	p := webhookPayload{
		Kind:        string(e.Kind),
		Context:     e.Context,
		ExecutionID: e.ExecutionID,
		Path:        e.Path,
		Size:        e.Size,
	}
	if e.Err != nil {
		p.Err = e.Err.Error()
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.Webhook.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.cfg.Webhook.AuthHeader != "" {
		req.Header.Set("Authorization", w.cfg.Webhook.AuthHeader)
	}
	resp, err := w.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook: %d %s", resp.StatusCode, bytes.TrimSpace(buf))
	}
	return nil
}
