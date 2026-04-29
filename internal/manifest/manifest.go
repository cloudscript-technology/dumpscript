// Package manifest defines the JSON sidecar file uploaded next to every
// successful dump. The manifest captures everything an operator (or a future
// Restore) needs to know about the artifact without re-fetching the file:
// who produced it, what engine, what schedule, when, how big, and a checksum
// for integrity verification.
package manifest

import (
	"encoding/json"
	"fmt"
	"time"
)

// Manifest is the JSON shape of the sidecar. Versioned via SchemaVersion so
// future fields can be added without breaking existing readers.
type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`

	// Identification
	ExecutionID  string `json:"executionId"`
	ScheduleName string `json:"scheduleName,omitempty"`

	// Source
	Engine    string `json:"engine"`
	DBName    string `json:"dbName,omitempty"`
	DBHost    string `json:"dbHost,omitempty"`
	Periodicity string `json:"periodicity,omitempty"`

	// Artifact
	Key             string    `json:"key"`
	SizeBytes       int64     `json:"sizeBytes"`
	Checksum        string    `json:"checksum,omitempty"`        // hex sha256
	ChecksumType    string    `json:"checksumType,omitempty"`    // "sha256"
	Compression     string    `json:"compression,omitempty"`     // "gzip" | "zstd"
	DumpOptions     string    `json:"dumpOptions,omitempty"`     // raw DUMP_OPTIONS at dump time

	// Timing
	StartedAt    time.Time `json:"startedAt"`
	CompletedAt  time.Time `json:"completedAt"`
	DurationSeconds float64 `json:"durationSeconds"`

	// Tooling
	DumpscriptVersion string `json:"dumpscriptVersion,omitempty"`
}

// CurrentSchemaVersion is the version stamped on every new manifest produced
// by this binary build. Bump it on backwards-incompatible changes; readers
// of older versions should skip-or-warn fields they don't recognize.
const CurrentSchemaVersion = 1

// Marshal returns the manifest as indented JSON ready to upload as a small
// sidecar object.
func (m *Manifest) Marshal() ([]byte, error) {
	if m.SchemaVersion == 0 {
		m.SchemaVersion = CurrentSchemaVersion
	}
	if m.ChecksumType == "" && m.Checksum != "" {
		m.ChecksumType = "sha256"
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	return b, nil
}

// SidecarKey returns the storage key for the manifest given the dump key.
// Convention: append `.manifest.json` so the manifest sorts alphabetically
// next to its dump in flat listings.
//
// Example:
//   dumpKey:      pg/daily/2026/04/29/dump_20260429_020000.sql.gz
//   manifestKey: pg/daily/2026/04/29/dump_20260429_020000.sql.gz.manifest.json
func SidecarKey(dumpKey string) string {
	return dumpKey + ".manifest.json"
}
