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
	Register("discord", func(cfg *config.Config, log *slog.Logger) (Notifier, bool) {
		if cfg.Discord.WebhookURL == "" {
			return nil, false
		}
		return NewDiscord(cfg, log), true
	})
}

// Discord is a Notifier that posts to a Discord Incoming Webhook.
// Payload shape: https://discord.com/developers/docs/resources/webhook
type Discord struct {
	cfg  *config.Config
	log  *slog.Logger
	http *http.Client
}

func NewDiscord(cfg *config.Config, log *slog.Logger) *Discord {
	return &Discord{
		cfg:  cfg,
		log:  log,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

type discordPayload struct {
	Content  string `json:"content"`
	Username string `json:"username,omitempty"`
}

func (d *Discord) Notify(ctx context.Context, e Event) error {
	if d.cfg.Discord.WebhookURL == "" {
		return nil
	}
	if e.Kind == EventSuccess && !d.cfg.Discord.NotifySuccess {
		return nil
	}
	body, err := json.Marshal(discordPayload{
		Content:  formatPlainMessage(e),
		Username: d.cfg.Discord.Username,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.cfg.Discord.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord webhook: %d %s", resp.StatusCode, bytes.TrimSpace(buf))
	}
	return nil
}

// formatPlainMessage renders a single-line text message suitable for any
// chat-style notifier (Discord, Teams, Webhook). Slack has its own
// rich-attachment formatter in slack.go.
func formatPlainMessage(e Event) string {
	emoji := ""
	switch e.Kind {
	case EventStart:
		emoji = "▶️"
	case EventSuccess:
		emoji = "✅"
	case EventFailure:
		emoji = "❌"
	case EventSkipped:
		emoji = "⏭️"
	}
	msg := fmt.Sprintf("%s dumpscript %s", emoji, e.Kind)
	if e.Context != "" {
		msg += " · " + e.Context
	}
	if e.ExecutionID != "" {
		msg += fmt.Sprintf(" · execution_id=%s", e.ExecutionID)
	}
	if e.Path != "" {
		msg += fmt.Sprintf(" · path=%s", e.Path)
	}
	if e.Size > 0 {
		msg += fmt.Sprintf(" · size=%d", e.Size)
	}
	if e.Err != nil {
		msg += fmt.Sprintf(" · err=%v", e.Err)
	}
	return msg
}
