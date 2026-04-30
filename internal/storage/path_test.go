package storage

import (
	"testing"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestBuildKey(t *testing.T) {
	ts := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		cfg      *config.Config
		filename string
		want     string
	}{
		{
			name: "s3 daily",
			cfg: &config.Config{
				Backend:     config.BackendS3,
				S3:          config.S3{Prefix: "postgresql-dumps"},
				Periodicity: config.Daily,
			},
			filename: "dump_20250324_120000.sql.gz",
			want:     "postgresql-dumps/daily/2025/03/24/dump_20250324_120000.sql.gz",
		},
		{
			name: "azure weekly",
			cfg: &config.Config{
				Backend:     config.BackendAzure,
				Azure:       config.Azure{Prefix: "az-prefix"},
				Periodicity: config.Weekly,
			},
			filename: "dump.sql.gz",
			want:     "az-prefix/weekly/2025/03/24/dump.sql.gz",
		},
		{
			name: "yearly",
			cfg: &config.Config{
				Backend: config.BackendS3, S3: config.S3{Prefix: "p"},
				Periodicity: config.Yearly,
			},
			filename: "d.sql.gz",
			want:     "p/yearly/2025/03/24/d.sql.gz",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildKey(tc.cfg, ts, tc.filename)
			if got != tc.want {
				t.Errorf("BuildKey = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildKey_SingleDigitDate(t *testing.T) {
	// January 2nd — must be zero-padded in the key.
	ts := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	cfg := &config.Config{
		Backend: config.BackendS3, S3: config.S3{Prefix: "x"}, Periodicity: config.Daily,
	}
	if got := BuildKey(cfg, ts, "f"); got != "x/daily/2024/01/02/f" {
		t.Errorf("got %q", got)
	}
}

func TestPeriodPrefix(t *testing.T) {
	tests := []struct {
		cfg  *config.Config
		want string
	}{
		{
			cfg:  &config.Config{Backend: config.BackendS3, S3: config.S3{Prefix: "dumps"}, Periodicity: config.Daily},
			want: "dumps/daily/",
		},
		{
			cfg:  &config.Config{Backend: config.BackendAzure, Azure: config.Azure{Prefix: "ap"}, Periodicity: config.Monthly},
			want: "ap/monthly/",
		},
	}
	for _, tc := range tests {
		if got := PeriodPrefix(tc.cfg); got != tc.want {
			t.Errorf("PeriodPrefix = %q, want %q", got, tc.want)
		}
	}
}

func TestLockKey(t *testing.T) {
	ts := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{
			name: "s3",
			cfg:  &config.Config{Backend: config.BackendS3, S3: config.S3{Prefix: "dumps"}, Periodicity: config.Daily},
			want: "dumps/daily/2025/03/24/.lock",
		},
		{
			name: "azure",
			cfg:  &config.Config{Backend: config.BackendAzure, Azure: config.Azure{Prefix: "ap"}, Periodicity: config.Weekly},
			want: "ap/weekly/2025/03/24/.lock",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LockKey(tc.cfg, ts); got != tc.want {
				t.Errorf("LockKey = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDayFolder(t *testing.T) {
	ts := time.Date(2025, 3, 24, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{Backend: config.BackendS3, S3: config.S3{Prefix: "x"}, Periodicity: config.Daily}
	if got := DayFolder(cfg, ts); got != "x/daily/2025/03/24/" {
		t.Errorf("DayFolder = %q", got)
	}
}
