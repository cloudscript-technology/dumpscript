package storage

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// Options control the wiring of the composed Storage (Logging → Retrying → base).
type Options struct {
	// AWSCredentials overrides the default credential chain for the S3 backend
	// (e.g., supply an IRSA-backed provider).
	AWSCredentials aws.CredentialsProvider
	// OnRetry is invoked between retry attempts (e.g., refresh credentials).
	OnRetry func(attempt int)
	// Retry controls retry behavior. Zero value uses DefaultRetryConfig.
	Retry RetryConfig
}

// Constructor builds the *base* Storage for a backend (no decorators).
// Each backend file registers itself in init() — adding a new backend =
// creating a new file with Register() in its init().
type Constructor func(ctx context.Context, cfg *config.Config, log *slog.Logger, opts Options) (Storage, error)

var (
	registryMu sync.RWMutex
	registry   = map[config.StorageBackend]Constructor{}
)

// Register installs ctor for the given backend. Must be called from init().
func Register(backend config.StorageBackend, ctor Constructor) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[backend] = ctor
}

// Registered lists all currently registered storage backends (sorted).
func Registered() []config.StorageBackend {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]config.StorageBackend, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// New builds the Storage for the active backend, wrapped with the standard
// Logging + Retrying decorators.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger, opts Options) (Storage, error) {
	registryMu.RLock()
	ctor, ok := registry[cfg.Backend]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no storage registered for backend=%q (registered: %v)",
			cfg.Backend, Registered())
	}
	base, err := ctor(ctx, cfg, log, opts)
	if err != nil {
		return nil, err
	}

	retryCfg := opts.Retry
	if retryCfg.MaxAttempts == 0 {
		retryCfg = DefaultRetryConfig()
	}

	return NewLogging(
		NewRetrying(base, retryCfg, log, opts.OnRetry),
		log,
	), nil
}
