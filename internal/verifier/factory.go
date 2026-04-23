package verifier

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// Noop is a Verifier that accepts any artifact. Used when VerifyContent=false.
type Noop struct{}

func (Noop) Verify(_ context.Context, _ string) error { return nil }

// Constructor builds a Verifier for a registered DBType.
type Constructor func(log *slog.Logger) Verifier

var (
	registryMu sync.RWMutex
	registry   = map[config.DBType]Constructor{}
)

// Register installs ctor for the given DBType. Must be called from init().
func Register(dbType config.DBType, ctor Constructor) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[dbType] = ctor
}

// New returns the Verifier Strategy for cfg.DB.Type, or Noop when
// cfg.VerifyContent is false.
func New(cfg *config.Config, log *slog.Logger) (Verifier, error) {
	if !cfg.VerifyContent {
		return Noop{}, nil
	}
	registryMu.RLock()
	ctor, ok := registry[cfg.DB.Type]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no verifier registered for DB_TYPE=%q (registered: %v)",
			cfg.DB.Type, Registered())
	}
	return ctor(log), nil
}

// Registered lists all currently registered DBTypes (sorted).
func Registered() []config.DBType {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]config.DBType, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
