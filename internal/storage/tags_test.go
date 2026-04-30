package storage

import (
	"net/url"
	"strings"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestBackupTags_AlwaysIncludesManagedBy(t *testing.T) {
	cfg := &config.Config{}
	tags := backupTags(cfg)
	if tags["managed_by"] != "dumpscript" {
		t.Fatalf("missing managed_by tag, got %v", tags)
	}
}

func TestBackupTags_IncludesEngineAndPeriodicity(t *testing.T) {
	cfg := &config.Config{}
	cfg.DB.Type = "postgresql"
	cfg.Periodicity = "daily"
	tags := backupTags(cfg)
	if tags["engine"] != "postgresql" {
		t.Errorf("engine = %q", tags["engine"])
	}
	if tags["periodicity"] != "daily" {
		t.Errorf("periodicity = %q", tags["periodicity"])
	}
}

func TestEncodeS3Tagging_DeterministicSortedOutput(t *testing.T) {
	tags := map[string]string{
		"zebra":      "z-value",
		"apple":      "a-value",
		"managed_by": "dumpscript",
	}
	got := encodeS3Tagging(tags)
	// Must be alphabetically sorted by key.
	want := "apple=a-value&managed_by=dumpscript&zebra=z-value"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestEncodeS3Tagging_URLEncodesSpecialChars(t *testing.T) {
	tags := map[string]string{"path": "a/b c"}
	got := encodeS3Tagging(tags)
	v, err := url.ParseQuery(got)
	if err != nil {
		t.Fatal(err)
	}
	if v.Get("path") != "a/b c" {
		t.Errorf("decoded path = %q, want a/b c", v.Get("path"))
	}
	if !strings.Contains(got, "%2F") && !strings.Contains(got, "%2f") {
		t.Errorf("expected '/' to be URL-escaped, got %q", got)
	}
}

func TestEncodeS3Tagging_EmptyMapEmptyString(t *testing.T) {
	if got := encodeS3Tagging(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := encodeS3Tagging(map[string]string{}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestMetadataPtrMap_NilForEmpty(t *testing.T) {
	if got := metadataPtrMap(nil); got != nil {
		t.Errorf("nil input should yield nil map, got %v", got)
	}
}

func TestMetadataPtrMap_PreservesValues(t *testing.T) {
	in := map[string]string{"engine": "postgres", "managed_by": "dumpscript"}
	got := metadataPtrMap(in)
	if got["engine"] == nil || *got["engine"] != "postgres" {
		t.Errorf("engine ptr = %v", got["engine"])
	}
	if got["managed_by"] == nil || *got["managed_by"] != "dumpscript" {
		t.Errorf("managed_by ptr = %v", got["managed_by"])
	}
}
