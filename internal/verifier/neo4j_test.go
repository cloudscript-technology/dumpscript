package verifier

import (
	"context"
	"strings"
	"testing"
)

func TestNeo4j_Verify_NonEmpty(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "n.neo4j.gz", "some neo4j archive bytes")
	if err := NewNeo4j(discardLogger()).Verify(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestNeo4j_Verify_Empty(t *testing.T) {
	dir := t.TempDir()
	p := writeGzip(t, dir, "n.neo4j.gz", "")
	err := NewNeo4j(discardLogger()).Verify(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("err = %v", err)
	}
}

func TestNeo4j_Verify_TruncatedGzip(t *testing.T) {
	dir := t.TempDir()
	p := writeTruncatedGzip(t, dir, "n.neo4j.gz", "some neo4j archive bytes")
	if err := NewNeo4j(discardLogger()).Verify(context.Background(), p); err == nil {
		t.Error("expected error for truncated gzip")
	}
}

func TestNeo4j_RegisteredInFactory(t *testing.T) {
	found := false
	for _, dbt := range Registered() {
		if dbt == "neo4j" {
			found = true
			break
		}
	}
	if !found {
		t.Error("neo4j not registered in verifier registry")
	}
}
