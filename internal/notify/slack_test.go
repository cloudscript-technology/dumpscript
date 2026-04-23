package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func baseCfg() *config.Config {
	return &config.Config{
		DB:          config.DB{Type: config.DBPostgres, Host: "h", Name: "db"},
		Periodicity: config.Daily,
		Slack: config.Slack{
			WebhookURL: "",
			Channel:    "#custom",
			Username:   "dumpscript-bot",
		},
	}
}

func TestSlack_Build_SuccessPayload(t *testing.T) {
	s := NewSlack(baseCfg(), quietLogger())
	p := s.build(Event{Kind: EventSuccess, Path: "s3://b/k", Size: 12345})

	if p.Channel != "#custom" {
		t.Errorf("channel = %q", p.Channel)
	}
	if p.Username != "dumpscript-bot" {
		t.Errorf("username = %q", p.Username)
	}
	if p.IconEmoji != ":white_check_mark:" {
		t.Errorf("icon = %q", p.IconEmoji)
	}
	if len(p.Attachments) != 1 {
		t.Fatalf("attachments = %d", len(p.Attachments))
	}
	a := p.Attachments[0]
	if a.Color != "good" {
		t.Errorf("color = %q", a.Color)
	}
	if a.Ts == 0 {
		t.Errorf("ts unset")
	}

	found := false
	for _, f := range a.Fields {
		if f.Title == "Location" && f.Value == "s3://b/k" {
			found = true
		}
	}
	if !found {
		t.Errorf("Location field missing in: %+v", a.Fields)
	}
}

func TestSlack_Build_FailurePayload(t *testing.T) {
	s := NewSlack(baseCfg(), quietLogger())
	p := s.build(Event{Kind: EventFailure, Err: errors.New("boom"), Context: "ctx"})
	if p.IconEmoji != ":warning:" {
		t.Errorf("icon = %q", p.IconEmoji)
	}
	a := p.Attachments[0]
	if a.Color != "danger" {
		t.Errorf("color = %q", a.Color)
	}
	if !strings.HasSuffix(a.Fallback, "boom") {
		t.Errorf("fallback = %q", a.Fallback)
	}
}

func TestSlack_Build_SkippedPayload(t *testing.T) {
	s := NewSlack(baseCfg(), quietLogger())
	p := s.build(Event{Kind: EventSkipped, ExecutionID: "abc123", Context: "Lock held at s3://b/daily/.lock"})
	if p.IconEmoji != ":no_entry:" {
		t.Errorf("icon = %q", p.IconEmoji)
	}
	a := p.Attachments[0]
	if a.Color != "warning" {
		t.Errorf("color = %q", a.Color)
	}
	if !strings.Contains(a.Title, "Skipped") {
		t.Errorf("title = %q", a.Title)
	}
	foundExec, foundCtx := false, false
	for _, f := range a.Fields {
		if f.Title == "Execution ID" && f.Value == "abc123" {
			foundExec = true
		}
		if f.Title == "Context" && strings.Contains(f.Value, "Lock held") {
			foundCtx = true
		}
	}
	if !foundExec {
		t.Error("Execution ID field missing")
	}
	if !foundCtx {
		t.Error("Context field missing")
	}
}

func TestSlack_Notify_SkippedSent(t *testing.T) {
	var captured slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := baseCfg()
	cfg.Slack.WebhookURL = srv.URL
	s := NewSlack(cfg, quietLogger())

	if err := s.Notify(context.Background(), Event{Kind: EventSkipped, ExecutionID: "id1", Context: "locked"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(captured.Attachments) == 0 || captured.Attachments[0].Color != "warning" {
		t.Errorf("payload missing/wrong: %+v", captured)
	}
}

func TestSlack_Build_DefaultChannel(t *testing.T) {
	cfg := baseCfg()
	cfg.Slack.Channel = ""
	cfg.Slack.Username = ""
	s := NewSlack(cfg, quietLogger())
	p := s.build(Event{Kind: EventStart})
	if p.Channel != "#alerts" {
		t.Errorf("default channel = %q", p.Channel)
	}
	if p.Username != "DumpScript Bot" {
		t.Errorf("default username = %q", p.Username)
	}
}

func TestSlack_Notify_NoWebhookNoop(t *testing.T) {
	cfg := baseCfg()
	s := NewSlack(cfg, quietLogger())
	if err := s.Notify(context.Background(), Event{Kind: EventFailure, Err: errors.New("e")}); err != nil {
		t.Errorf("unexpected: %v", err)
	}
}

func TestSlack_Notify_SuccessGating(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := baseCfg()
	cfg.Slack.WebhookURL = srv.URL
	cfg.Slack.NotifySuccess = false

	s := NewSlack(cfg, quietLogger())
	if err := s.Notify(context.Background(), Event{Kind: EventSuccess}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if called {
		t.Error("expected NO request when NotifySuccess=false")
	}
}

func TestSlack_Notify_SendsAndParsesJSON(t *testing.T) {
	var captured slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := baseCfg()
	cfg.Slack.WebhookURL = srv.URL
	s := NewSlack(cfg, quietLogger())

	if err := s.Notify(context.Background(), Event{Kind: EventFailure, Err: errors.New("e"), Context: "c"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(captured.Attachments) != 1 {
		t.Fatalf("no attachment in payload: %+v", captured)
	}
	if captured.Attachments[0].Color != "danger" {
		t.Errorf("color = %q", captured.Attachments[0].Color)
	}
}

func TestSlack_Notify_PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := baseCfg()
	cfg.Slack.WebhookURL = srv.URL
	s := NewSlack(cfg, quietLogger())

	err := s.Notify(context.Background(), Event{Kind: EventFailure, Err: errors.New("x")})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "a", "b"); got != "a" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmpty("first"); got != "first" {
		t.Errorf("got %q", got)
	}
}

func TestErrString(t *testing.T) {
	if got := errString(nil); got != "" {
		t.Errorf("nil err should be empty, got %q", got)
	}
	if got := errString(errors.New("boom")); got != "boom" {
		t.Errorf("got %q", got)
	}
}
