// Package retention deletes backup objects older than a configured age.
package retention

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/storage"
)

// datePathRe matches the YYYY/MM/DD segment inside a backup object key.
// Example: "postgresql-dumps/daily/2025/03/24/dump_20250324_120000.sql.gz"
var datePathRe = regexp.MustCompile(`/(\d{4})/(\d{2})/(\d{2})/`)

// backupFileRe matches valid backup artifacts (*.sql[.gz] or *.archive[.gz]).
var backupFileRe = regexp.MustCompile(`\.(sql|archive)(\.gz)?$`)

// Cleaner applies retention policy against a Storage.
type Cleaner struct {
	store storage.Storage
	log   *slog.Logger
}

func New(store storage.Storage, log *slog.Logger) *Cleaner {
	return &Cleaner{store: store, log: log}
}

// Result is a summary of a retention run.
type Result struct {
	Deleted int
	Kept    int
	Skipped int
}

// Run deletes objects under prefix whose embedded path-date is older than retentionDays.
// The date is parsed from YYYY/MM/DD in the key (not from object metadata), matching
// the bash implementation and staying robust to object-copy events that reset LastModified.
func (c *Cleaner) Run(ctx context.Context, prefix string, retentionDays int, now time.Time) (Result, error) {
	var r Result
	if retentionDays <= 0 {
		c.log.Info("retention disabled", "retention_days", retentionDays)
		return r, nil
	}
	cutoff := now.AddDate(0, 0, -retentionDays)
	cutoffStr := cutoff.UTC().Format("2006-01-02")
	c.log.Info("retention cleanup",
		"prefix", prefix, "retention_days", retentionDays, "cutoff", cutoffStr)

	objs, err := c.store.List(ctx, prefix)
	if err != nil {
		return r, err
	}

	for _, o := range objs {
		if !backupFileRe.MatchString(o.Key) {
			r.Skipped++
			continue
		}
		m := datePathRe.FindStringSubmatch(o.Key)
		if len(m) != 4 {
			c.log.Debug("retention: no date segment in key", "key", o.Key)
			r.Skipped++
			continue
		}
		backupDate := m[1] + "-" + m[2] + "-" + m[3]
		if backupDate < cutoffStr {
			c.log.Info("retention delete", "key", o.Key, "backup_date", backupDate)
			if err := c.store.Delete(ctx, o.Key); err != nil {
				c.log.Warn("retention delete failed", "key", o.Key, "err", err)
				continue
			}
			r.Deleted++
		} else {
			r.Kept++
		}
	}
	c.log.Info("retention cleanup done", "deleted", r.Deleted, "kept", r.Kept, "skipped", r.Skipped)
	return r, nil
}
