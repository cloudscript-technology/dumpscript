package cli

import (
	"io"
	"log/slog"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
	"github.com/cloudscript-technology/dumpscript/internal/notify"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestNewRoot_RegistersSubcommands(t *testing.T) {
	root := NewRoot()
	want := map[string]bool{"dump": false, "restore": false, "cleanup": false}
	for _, c := range root.Commands() {
		name := c.Name()
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for cmd, found := range want {
		if !found {
			t.Errorf("subcommand %q not registered", cmd)
		}
	}
}

func TestNewRoot_HasLogLevelFlag(t *testing.T) {
	root := NewRoot()
	if root.PersistentFlags().Lookup("log-level") == nil {
		t.Error("log-level persistent flag missing")
	}
}

func TestBuildNotifier_ChoosesCorrectType(t *testing.T) {
	t.Run("no webhook → noop", func(t *testing.T) {
		cfg := &config.Config{}
		n := buildNotifier(cfg, quietLogger())
		if _, ok := n.(notify.Noop); !ok {
			t.Errorf("got %T, want Noop", n)
		}
	})
	t.Run("webhook set → slack", func(t *testing.T) {
		cfg := &config.Config{Slack: config.Slack{WebhookURL: "https://hooks.slack.com/services/x"}}
		n := buildNotifier(cfg, quietLogger())
		if _, ok := n.(*notify.Slack); !ok {
			t.Errorf("got %T, want *notify.Slack", n)
		}
	})
}
