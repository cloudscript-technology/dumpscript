package verifier

import (
	"context"
	"strings"
	"testing"
)

const esValidNDJSON = `{"_id":"a","_source":{"k":1}}` + "\n" +
	`{"_id":"b","_source":{"k":2}}` + "\n"

const esTruncatedNDJSON = `{"_id":"a","_source":{"k":1}}` + "\n" +
	`{"_id":"b","_source":{"k":2`

func TestElasticsearch_Verify_Valid(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "es.ndjson.gz", esValidNDJSON)
	if err := NewElasticsearch(discardLogger()).Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestElasticsearch_Verify_TruncatedLastLine(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "es.ndjson.gz", esTruncatedNDJSON)
	err := NewElasticsearch(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("err = %v", err)
	}
}

func TestElasticsearch_Verify_Empty(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "es.ndjson.gz", "")
	err := NewElasticsearch(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("err = %v", err)
	}
}

func TestElasticsearch_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "es.ndjson.gz", esValidNDJSON)
	if err := NewElasticsearch(discardLogger()).Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}

func TestElasticsearch_RegisteredInFactory(t *testing.T) {
	found := false
	for _, dbt := range Registered() {
		if dbt == "elasticsearch" {
			found = true
			break
		}
	}
	if !found {
		t.Error("elasticsearch not registered in verifier registry")
	}
}
