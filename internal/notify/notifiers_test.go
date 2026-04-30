package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// captureWebhook returns a httptest.Server that records the most recent
// request body it received.
func captureWebhook(t *testing.T, status int) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	body := &bytes.Buffer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body.Reset()
		_, _ = io.Copy(body, r.Body)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, body
}

// ---------------- Discord ----------------

func TestDiscord_Notify_PostsContent(t *testing.T) {
	srv, body := captureWebhook(t, http.StatusNoContent)
	cfg := &config.Config{Discord: config.Discord{
		WebhookURL: srv.URL, Username: "bot",
		NotifySuccess: true,
	}}
	d := NewDiscord(cfg, quietLogger())
	if err := d.Notify(context.Background(), Event{Kind: EventSuccess, Path: "pg/x.sql.gz", Size: 123, ExecutionID: "abc"}); err != nil {
		t.Fatal(err)
	}
	var p discordPayload
	if err := json.Unmarshal(body.Bytes(), &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if !strings.Contains(p.Content, "success") || !strings.Contains(p.Content, "abc") {
		t.Errorf("content = %q, want kind+execution_id", p.Content)
	}
	if p.Username != "bot" {
		t.Errorf("username = %q, want bot", p.Username)
	}
}

func TestDiscord_Notify_SuppressesSuccessByDefault(t *testing.T) {
	srv, body := captureWebhook(t, http.StatusOK)
	cfg := &config.Config{Discord: config.Discord{WebhookURL: srv.URL}} // NotifySuccess=false
	d := NewDiscord(cfg, quietLogger())
	if err := d.Notify(context.Background(), Event{Kind: EventSuccess}); err != nil {
		t.Fatal(err)
	}
	if body.Len() != 0 {
		t.Errorf("body should be empty (no POST sent), got %q", body.String())
	}
}

func TestDiscord_Notify_PropagatesNon2xx(t *testing.T) {
	srv, _ := captureWebhook(t, http.StatusInternalServerError)
	cfg := &config.Config{Discord: config.Discord{WebhookURL: srv.URL, NotifySuccess: true}}
	err := NewDiscord(cfg, quietLogger()).Notify(context.Background(), Event{Kind: EventFailure})
	if err == nil {
		t.Fatal("expected error from 500")
	}
	if !strings.Contains(err.Error(), "discord webhook") {
		t.Errorf("err=%v, want containing 'discord webhook'", err)
	}
}

// ---------------- Teams ----------------

func TestTeams_Notify_PostsMessageCard(t *testing.T) {
	srv, body := captureWebhook(t, http.StatusOK)
	cfg := &config.Config{Teams: config.Teams{WebhookURL: srv.URL, NotifySuccess: true}}
	tn := NewTeams(cfg, quietLogger())
	if err := tn.Notify(context.Background(), Event{Kind: EventFailure, Context: "postgres", ExecutionID: "ex1"}); err != nil {
		t.Fatal(err)
	}
	var p teamsPayload
	if err := json.Unmarshal(body.Bytes(), &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if p.Type != "MessageCard" {
		t.Errorf("@type=%q, want MessageCard", p.Type)
	}
	if p.ThemeColor != "FF4136" { // failure red
		t.Errorf("themeColor=%q, want FF4136", p.ThemeColor)
	}
	if !strings.Contains(p.Text, "ex1") {
		t.Errorf("text=%q, want execution_id", p.Text)
	}
}

func TestTeams_Color_PerKind(t *testing.T) {
	cases := []struct {
		k    EventKind
		want string
	}{
		{EventStart, "0074D9"},
		{EventSuccess, "2ECC40"},
		{EventFailure, "FF4136"},
		{EventSkipped, "FFDC00"},
	}
	for _, c := range cases {
		if got := teamsColor(c.k); got != c.want {
			t.Errorf("teamsColor(%s)=%s, want %s", c.k, got, c.want)
		}
	}
}

// ---------------- Webhook ----------------

func TestWebhook_Notify_PostsJSON(t *testing.T) {
	srv, body := captureWebhook(t, http.StatusOK)
	cfg := &config.Config{Webhook: config.Webhook{
		URL: srv.URL, AuthHeader: "Bearer xyz",
		NotifySuccess: true,
	}}
	w := NewWebhook(cfg, quietLogger())
	if err := w.Notify(context.Background(), Event{Kind: EventSuccess, Path: "k", Size: 99, ExecutionID: "e1"}); err != nil {
		t.Fatal(err)
	}
	var p webhookPayload
	if err := json.Unmarshal(body.Bytes(), &p); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if p.Kind != "success" || p.Path != "k" || p.Size != 99 || p.ExecutionID != "e1" {
		t.Errorf("payload = %+v", p)
	}
}

func TestWebhook_AuthHeaderPresent(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	cfg := &config.Config{Webhook: config.Webhook{
		URL: srv.URL, AuthHeader: "Bearer xyz", NotifySuccess: true,
	}}
	if err := NewWebhook(cfg, quietLogger()).Notify(context.Background(), Event{Kind: EventStart}); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer xyz" {
		t.Errorf("Authorization=%q, want 'Bearer xyz'", got)
	}
}

// ---------------- Stdout ----------------

func TestStdout_Notify_WritesJSONLine(t *testing.T) {
	cfg := &config.Config{NotifyStdout: config.NotifyStdout{Enabled: true, NotifySuccess: true}}
	s := NewStdout(cfg, quietLogger())
	var buf bytes.Buffer
	s.w = &buf
	if err := s.Notify(context.Background(), Event{Kind: EventSuccess, Path: "k", Size: 42, ExecutionID: "e2"}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output not newline-terminated: %q", out)
	}
	var p stdoutPayload
	if err := json.Unmarshal([]byte(strings.TrimSuffix(out, "\n")), &p); err != nil {
		t.Fatalf("not valid JSON: %v / out=%q", err, out)
	}
	if p.Event != "success" || p.Size != 42 || p.ExecutionID != "e2" {
		t.Errorf("payload = %+v", p)
	}
}

func TestStdout_Notify_RespectsSuppressSuccess(t *testing.T) {
	cfg := &config.Config{NotifyStdout: config.NotifyStdout{Enabled: true, NotifySuccess: false}}
	s := NewStdout(cfg, quietLogger())
	var buf bytes.Buffer
	s.w = &buf
	if err := s.Notify(context.Background(), Event{Kind: EventSuccess}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("buffer should be empty, got %q", buf.String())
	}
	if err := s.Notify(context.Background(), Event{Kind: EventFailure}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"event":"failure"`) {
		t.Errorf("failure should pass through: %q", buf.String())
	}
}

func TestStdout_Disabled_NoOp(t *testing.T) {
	cfg := &config.Config{NotifyStdout: config.NotifyStdout{Enabled: false}}
	s := NewStdout(cfg, quietLogger())
	var buf bytes.Buffer
	s.w = &buf
	_ = s.Notify(context.Background(), Event{Kind: EventFailure})
	if buf.Len() != 0 {
		t.Errorf("disabled stdout should not write, got %q", buf.String())
	}
}
