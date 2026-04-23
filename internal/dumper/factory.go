package dumper

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// Constructor builds a Dumper for a registered DBType.
// Each engine file (postgres.go, mysql.go, ...) registers itself from an
// init() so adding a new engine = creating a new file + one init call.
type Constructor func(cfg *config.Config, log *slog.Logger) Dumper

var (
	registryMu sync.RWMutex
	registry   = map[config.DBType]Constructor{}
)

// Register installs ctor for the given DBType. Must be called from init() so
// all engines are available before the first New() call. Idempotent — later
// registrations overwrite (useful for tests).
func Register(dbType config.DBType, ctor Constructor) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[dbType] = ctor
}

// New returns the Dumper Strategy registered for cfg.DB.Type.
func New(cfg *config.Config, log *slog.Logger) (Dumper, error) {
	registryMu.RLock()
	ctor, ok := registry[cfg.DB.Type]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no dumper registered for DB_TYPE=%q (registered: %v)",
			cfg.DB.Type, Registered())
	}
	return ctor(cfg, log), nil
}

// Registered lists all currently registered DBTypes (sorted, for determinism).
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
