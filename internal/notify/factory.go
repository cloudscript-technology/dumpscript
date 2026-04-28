package notify

import (
	"log/slog"
	"sort"
	"sync"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// Constructor builds a Notifier when the corresponding env-var block is
// configured. Returning (nil, false) means "this notifier is not enabled
// for this config" — the registry skips it silently.
type Constructor func(cfg *config.Config, log *slog.Logger) (Notifier, bool)

var (
	registryMu sync.RWMutex
	registry   = map[string]Constructor{}
)

// Register installs ctor under name. Must be called from each notifier's
// init() so all candidates are visible before the first New() call.
// Idempotent — later calls overwrite (useful for tests).
func Register(name string, ctor Constructor) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = ctor
}

// New scans every registered Constructor; instantiates the ones that report
// "enabled=true" for the given config; returns:
//   - Noop if none enabled
//   - the single instance if exactly one enabled
//   - a Multi fan-out wrapping all enabled instances otherwise
func New(cfg *config.Config, log *slog.Logger) Notifier {
	registryMu.RLock()
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	sort.Strings(names) // deterministic order for tests + logs
	ctors := make([]Constructor, len(names))
	for i, n := range names {
		ctors[i] = registry[n]
	}
	registryMu.RUnlock()

	enabled := make([]Notifier, 0, len(ctors))
	enabledNames := make([]string, 0, len(ctors))
	retryCfg := DefaultRetryConfig()
	for i, c := range ctors {
		n, ok := c(cfg, log)
		if !ok || n == nil {
			continue
		}
		// Wrap each enabled notifier with retry so a transient 5xx from
		// Slack/Discord/Teams/Webhook doesn't drop the notification on the
		// floor on the first hiccup. Stdout doesn't fail in practice but
		// going through the decorator is harmless.
		n = NewRetrying(n, retryCfg, log, names[i])
		enabled = append(enabled, n)
		enabledNames = append(enabledNames, names[i])
	}

	switch len(enabled) {
	case 0:
		return Noop{}
	case 1:
		log.Debug("notifier active", "channel", enabledNames[0])
		return enabled[0]
	default:
		log.Debug("multi-channel notifier active", "channels", enabledNames)
		return &Multi{children: enabled, log: log}
	}
}

// Registered returns the names of every Constructor in registration order
// (sorted for determinism). Useful for diagnostics.
func Registered() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
