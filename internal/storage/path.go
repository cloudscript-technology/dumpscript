package storage

import (
	"fmt"
	"path"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// BuildKey assembles the object key: <prefix>/<periodicity>/YYYY/MM/DD/<filename>.
// Matches the layout produced by the original bash scripts for backward compatibility.
func BuildKey(cfg *config.Config, now time.Time, filename string) string {
	return path.Join(
		cfg.Prefix(),
		string(cfg.Periodicity),
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", int(now.Month())),
		fmt.Sprintf("%02d", now.Day()),
		filename,
	)
}

// PeriodPrefix returns the prefix scoped to a periodicity folder (<prefix>/<periodicity>/).
// Used by retention cleanup to list only the right set of objects.
func PeriodPrefix(cfg *config.Config) string {
	return path.Join(cfg.Prefix(), string(cfg.Periodicity)) + "/"
}

// DayFolder returns <prefix>/<periodicity>/YYYY/MM/DD/ — the destination folder
// shared by every execution for the given run date.
func DayFolder(cfg *config.Config, now time.Time) string {
	return path.Join(
		cfg.Prefix(),
		string(cfg.Periodicity),
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", int(now.Month())),
		fmt.Sprintf("%02d", now.Day()),
	) + "/"
}

// LockKey returns the day-level lock file key: <prefix>/<periodicity>/YYYY/MM/DD/.lock
// Used to serialize concurrent backup runs on the same day.
func LockKey(cfg *config.Config, now time.Time) string {
	return path.Join(
		cfg.Prefix(),
		string(cfg.Periodicity),
		fmt.Sprintf("%04d", now.Year()),
		fmt.Sprintf("%02d", int(now.Month())),
		fmt.Sprintf("%02d", now.Day()),
		".lock",
	)
}
