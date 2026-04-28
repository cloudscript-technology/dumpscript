package verifier

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// PostRestore performs a lightweight reachability check after a Restore
// finishes, so a successful Restore phase only flips when the target database
// is actually accepting connections again. The check is intentionally
// minimal — a TCP dial of the configured host:port within a short timeout —
// because anything richer (SELECT 1, db.runCommand) would require duplicating
// the credentials handling already implemented in dumper/restorer.
//
// File-based engines (sqlite) skip the check entirely. Engines that the
// operator/binary doesn't manage as a network service (sqlite has Host="")
// also skip.
//
// Returns nil on success or unsupported engine; returns an error when the
// configured endpoint is unreachable within the timeout.
func PostRestore(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	if cfg.DB.Type == config.DBSQLite {
		log.Debug("post-restore verifier: sqlite is file-based, skipping TCP check")
		return nil
	}
	if cfg.DB.Host == "" {
		log.Debug("post-restore verifier: no host configured, skipping")
		return nil
	}
	port := cfg.DB.Port
	if port == 0 {
		log.Debug("post-restore verifier: no port configured, skipping")
		return nil
	}
	addr := net.JoinHostPort(cfg.DB.Host, strconv.Itoa(port))

	// Use a per-attempt timeout that respects ctx cancellation.
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	d := net.Dialer{}
	conn, err := d.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("post-restore: %s unreachable: %w", addr, err)
	}
	_ = conn.Close()
	log.Info("post-restore verifier: target reachable", "addr", addr)
	return nil
}
