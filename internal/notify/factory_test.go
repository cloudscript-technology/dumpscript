package notify

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// fakeNotifier records every Notify call and can be told to return an error.
type fakeNotifier struct {
	calls atomic.Int64
	err   error
}

func (f *fakeNotifier) Notify(_ context.Context, _ Event) error {
	f.calls.Add(1)
	return f.err
}

func TestFactory_RegisteredIncludesAllBuiltInNotifiers(t *testing.T) {
	got := Registered()
	want := []string{"discord", "slack", "stdout", "teams", "webhook"}
	sort.Strings(got)
	have := make(map[string]bool, len(got))
	for _, n := range got {
		have[n] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing built-in notifier %q in %v", w, got)
		}
	}
}

func TestFactory_NoneEnabled_ReturnsNoop(t *testing.T) {
	cfg := &config.Config{} // every notifier reports !enabled
	n := New(cfg, quietLogger())
	if _, ok := n.(Noop); !ok {
		t.Errorf("got %T, want Noop", n)
	}
}

func TestFactory_OneEnabled_ReturnsThatNotifier(t *testing.T) {
	cfg := &config.Config{Slack: config.Slack{WebhookURL: "https://hooks.example/abc"}}
	n := New(cfg, quietLogger())
	// Factory wraps every enabled notifier in a Retrying decorator (see
	// retry.go); unwrap before asserting the underlying type.
	r, ok := n.(*Retrying)
	if !ok {
		t.Fatalf("got %T, want *Retrying", n)
	}
	if _, ok := r.Inner().(*Slack); !ok {
		t.Errorf("inner = %T, want *Slack", r.Inner())
	}
}

func TestFactory_MultipleEnabled_ReturnsMulti(t *testing.T) {
	cfg := &config.Config{
		Slack:        config.Slack{WebhookURL: "https://hooks.example/slack"},
		Discord:      config.Discord{WebhookURL: "https://hooks.example/discord"},
		NotifyStdout: config.NotifyStdout{Enabled: true},
	}
	n := New(cfg, quietLogger())
	m, ok := n.(*Multi)
	if !ok {
		t.Fatalf("got %T, want *Multi", n)
	}
	if len(m.children) != 3 {
		t.Errorf("children=%d, want 3", len(m.children))
	}
}

func TestFactory_RegisterIsIdempotent(t *testing.T) {
	const name = "custom-test-idempotent"
	Register(name, func(*config.Config, *slog.Logger) (Notifier, bool) { return Noop{}, true })
	Register(name, func(*config.Config, *slog.Logger) (Notifier, bool) { return Noop{}, true })
	count := 0
	for _, r := range Registered() {
		if r == name {
			count++
		}
	}
	if count != 1 {
		t.Errorf("registered %d copies of %q, want 1", count, name)
	}
}

// ---------------- Multi ----------------

func TestMulti_FansOutToAllChildren(t *testing.T) {
	a := &fakeNotifier{}
	b := &fakeNotifier{}
	c := &fakeNotifier{}
	m := NewMulti(quietLogger(), a, b, c)
	if err := m.Notify(context.Background(), Event{Kind: EventStart}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if a.calls.Load() != 1 || b.calls.Load() != 1 || c.calls.Load() != 1 {
		t.Errorf("calls a=%d b=%d c=%d, want all 1",
			a.calls.Load(), b.calls.Load(), c.calls.Load())
	}
}

func TestMulti_OneFailureDoesNotShortCircuit(t *testing.T) {
	a := &fakeNotifier{err: errors.New("a failed")}
	b := &fakeNotifier{} // succeeds
	c := &fakeNotifier{err: errors.New("c failed")}
	m := NewMulti(quietLogger(), a, b, c)

	err := m.Notify(context.Background(), Event{Kind: EventFailure})
	if err == nil {
		t.Fatal("expected joined error")
	}
	if b.calls.Load() != 1 {
		t.Errorf("middle (succeeding) child not invoked: calls=%d", b.calls.Load())
	}
	if c.calls.Load() != 1 {
		t.Errorf("third child after a failure was skipped — short-circuit bug")
	}
	got := err.Error()
	if !strings.Contains(got, "a failed") || !strings.Contains(got, "c failed") {
		t.Errorf("err = %q, want both children's messages", got)
	}
}

func TestMulti_EmptyChildren_NoOp(t *testing.T) {
	m := NewMulti(quietLogger())
	if err := m.Notify(context.Background(), Event{Kind: EventStart}); err != nil {
		t.Errorf("empty Multi should be no-op, got %v", err)
	}
}
