package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register("slack", func(cfg *config.Config, log *slog.Logger) (Notifier, bool) {
		if cfg.Slack.WebhookURL == "" {
			return nil, false
		}
		return NewSlack(cfg, log), true
	})
}

// Slack is a Notifier that posts to a Slack incoming webhook.
type Slack struct {
	cfg  *config.Config
	log  *slog.Logger
	http *http.Client
}

func NewSlack(cfg *config.Config, log *slog.Logger) *Slack {
	return &Slack{
		cfg:  cfg,
		log:  log,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Slack) Notify(ctx context.Context, e Event) error {
	if s.cfg.Slack.WebhookURL == "" {
		return nil
	}
	if e.Kind == EventSuccess && !s.cfg.Slack.NotifySuccess {
		return nil
	}
	buf, err := json.Marshal(s.build(e))
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Slack.WebhookURL, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.http.Do(req)
	if err != nil {
		s.log.Warn("slack post failed", "err", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

type slackPayload struct {
	Channel     string       `json:"channel,omitempty"`
	Username    string       `json:"username,omitempty"`
	IconEmoji   string       `json:"icon_emoji,omitempty"`
	Text        string       `json:"text,omitempty"`
	Attachments []attachment `json:"attachments,omitempty"`
}

type attachment struct {
	Color    string  `json:"color"`
	Fallback string  `json:"fallback,omitempty"`
	Title    string  `json:"title"`
	Fields   []field `json:"fields,omitempty"`
	Footer   string  `json:"footer,omitempty"`
	Ts       int64   `json:"ts"`
}

type field struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

func (s *Slack) build(e Event) slackPayload {
	host, _ := os.Hostname()
	ts := time.Now()
	base := []field{
		{Title: "Database Type", Value: string(s.cfg.DB.Type), Short: true},
		{Title: "Database Host", Value: s.cfg.DB.Host, Short: true},
		{Title: "Database Name", Value: s.cfg.DB.Name, Short: true},
		{Title: "Backup Frequency", Value: string(s.cfg.Periodicity), Short: true},
		{Title: "Hostname", Value: host, Short: true},
	}

	p := slackPayload{
		Channel:  firstNonEmpty(s.cfg.Slack.Channel, "#alerts"),
		Username: firstNonEmpty(s.cfg.Slack.Username, "DumpScript Bot"),
	}

	if e.ExecutionID != "" {
		base = append(base, field{Title: "Execution ID", Value: e.ExecutionID, Short: true})
	}

	switch e.Kind {
	case EventSuccess:
		p.IconEmoji = ":white_check_mark:"
		p.Attachments = []attachment{{
			Color:    "good",
			Fallback: "Database Backup Completed Successfully",
			Title:    ":heavy_check_mark: Database Backup Completed",
			Fields: append(base,
				field{Title: "Location", Value: e.Path, Short: false},
				field{Title: "Backup Size", Value: fmt.Sprintf("%d bytes", e.Size), Short: true},
				field{Title: "Timestamp", Value: ts.UTC().Format("2006-01-02 15:04:05 UTC"), Short: true},
			),
			Footer: "DumpScript Monitoring",
			Ts:     ts.Unix(),
		}}
	case EventFailure:
		p.IconEmoji = ":warning:"
		p.Attachments = []attachment{{
			Color:    "danger",
			Fallback: "Database Backup Failed: " + errString(e.Err),
			Title:    ":exclamation: Database Backup Failure",
			Fields: append(base,
				field{Title: "Error", Value: errString(e.Err), Short: false},
				field{Title: "Context", Value: e.Context, Short: false},
				field{Title: "Timestamp", Value: ts.UTC().Format("2006-01-02 15:04:05 UTC"), Short: true},
			),
			Footer: "DumpScript Monitoring",
			Ts:     ts.Unix(),
		}}
	case EventSkipped:
		p.IconEmoji = ":no_entry:"
		p.Attachments = []attachment{{
			Color:    "warning",
			Fallback: "Database Backup Skipped: another run in progress",
			Title:    ":lock: Database Backup Skipped",
			Fields: append(base,
				field{Title: "Reason", Value: "Lock file already present; another run is in progress", Short: false},
				field{Title: "Context", Value: e.Context, Short: false},
				field{Title: "Timestamp", Value: ts.UTC().Format("2006-01-02 15:04:05 UTC"), Short: true},
			),
			Footer: "DumpScript Monitoring",
			Ts:     ts.Unix(),
		}}
	case EventStart:
		p.IconEmoji = ":hourglass_flowing_sand:"
		p.Text = "Backup starting"
	}
	return p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func errString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
