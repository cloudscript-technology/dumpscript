package manifest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMarshal_StampsSchemaVersionWhenZero(t *testing.T) {
	m := &Manifest{Engine: "postgresql"}
	b, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("SchemaVersion not stamped: %d", m.SchemaVersion)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["schemaVersion"].(float64) != float64(CurrentSchemaVersion) {
		t.Errorf("schemaVersion in JSON = %v", got["schemaVersion"])
	}
}

func TestMarshal_PreservesUserSetSchemaVersion(t *testing.T) {
	m := &Manifest{SchemaVersion: 99, Engine: "postgresql"}
	if _, err := m.Marshal(); err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 99 {
		t.Errorf("user-set SchemaVersion overwritten: %d", m.SchemaVersion)
	}
}

func TestMarshal_DefaultsChecksumTypeWhenChecksumPresent(t *testing.T) {
	m := &Manifest{Engine: "postgresql", Checksum: "abc123"}
	if _, err := m.Marshal(); err != nil {
		t.Fatal(err)
	}
	if m.ChecksumType != "sha256" {
		t.Errorf("ChecksumType = %q, want sha256", m.ChecksumType)
	}
}

func TestMarshal_NoChecksumTypeWhenChecksumEmpty(t *testing.T) {
	m := &Manifest{Engine: "postgresql"}
	if _, err := m.Marshal(); err != nil {
		t.Fatal(err)
	}
	if m.ChecksumType != "" {
		t.Errorf("ChecksumType set without Checksum: %q", m.ChecksumType)
	}
}

func TestMarshal_FullStructProducesValidJSON(t *testing.T) {
	now := time.Date(2026, 4, 29, 2, 0, 0, 0, time.UTC)
	m := &Manifest{
		ExecutionID:     "exec-abc",
		ScheduleName:    "postgres-prod",
		Engine:          "postgresql",
		DBName:          "app",
		DBHost:          "pg.svc",
		Periodicity:     "daily",
		Key:             "pg/daily/2026/04/29/dump_X.sql.gz",
		SizeBytes:       1234567,
		Checksum:        "deadbeef",
		Compression:     "gzip",
		DumpOptions:     "--no-owner",
		StartedAt:       now,
		CompletedAt:     now.Add(2 * time.Minute),
		DurationSeconds: 120,
		DumpscriptVersion: "v0.1.0",
	}
	b, err := m.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var round Manifest
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("round-trip parse: %v\n%s", err, b)
	}
	if round.Engine != "postgresql" || round.Key != m.Key {
		t.Errorf("round-trip mismatch: %+v", round)
	}
	if !strings.Contains(string(b), `"checksumType": "sha256"`) {
		t.Errorf("expected checksumType field in JSON, got:\n%s", b)
	}
}

func TestSidecarKey_AppendsSuffix(t *testing.T) {
	got := SidecarKey("pg/daily/2026/04/29/dump_X.sql.gz")
	want := "pg/daily/2026/04/29/dump_X.sql.gz.manifest.json"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
