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
	Register("teams", func(cfg *config.Config, log *slog.Logger) (Notifier, bool) {
		if cfg.Teams.WebhookURL == "" {
			return nil, false
		}
		return NewTeams(cfg, log), true
	})
}

// Teams is a Notifier that posts to a Microsoft Teams Incoming Webhook
// using the legacy "MessageCard" connector format
// (https://learn.microsoft.com/en-us/microsoftteams/platform/webhooks-and-connectors/how-to/connectors-using).
type Teams struct {
	cfg  *config.Config
	log  *slog.Logger
	http *http.Client
}

func NewTeams(cfg *config.Config, log *slog.Logger) *Teams {
	return &Teams{
		cfg:  cfg,
		log:  log,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

type teamsPayload struct {
	Type       string `json:"@type"`
	Context    string `json:"@context"`
	Summary    string `json:"summary"`
	ThemeColor string `json:"themeColor,omitempty"`
	Title      string `json:"title"`
	Text       string `json:"text"`
}

func (t *Teams) Notify(ctx context.Context, e Event) error {
	if t.cfg.Teams.WebhookURL == "" {
		return nil
	}
	if e.Kind == EventSuccess && !t.cfg.Teams.NotifySuccess {
		return nil
	}
	body, err := json.Marshal(teamsPayload{
		Type:       "MessageCard",
		Context:    "https://schema.org/extensions",
		Summary:    fmt.Sprintf("dumpscript %s", e.Kind),
		ThemeColor: teamsColor(e.Kind),
		Title:      fmt.Sprintf("dumpscript %s", e.Kind),
		Text:       formatPlainMessage(e),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.Teams.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("teams webhook: %d %s", resp.StatusCode, bytes.TrimSpace(buf))
	}
	return nil
}

func teamsColor(k EventKind) string {
	switch k {
	case EventSuccess:
		return "2ECC40" // green
	case EventFailure:
		return "FF4136" // red
	case EventSkipped:
		return "FFDC00" // yellow
	default:
		return "0074D9" // blue (start / unknown)
	}
}
