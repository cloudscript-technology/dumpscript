package restorer

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

const esNDJSON = `{"_id":"a","_source":{"k":1}}` + "\n" +
	`{"_id":"b","_source":{"k":2}}` + "\n"

func TestElasticsearch_Restore_PostsBulk(t *testing.T) {
	var (
		mu       sync.Mutex
		bodies   []string
		bulkHits int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_bulk" || r.Method != "POST" {
			http.Error(w, r.URL.Path, http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		bulkHits++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":false,"items":[]}`))
	}))
	t.Cleanup(srv.Close)

	host, port := hostPortFromRestorerURL(t, srv.URL)
	gzPath := writeGzippedBytes(t, "es.ndjson.gz", esNDJSON)
	cfg := &config.Config{DB: config.DB{
		Type: config.DBElasticsearch, Host: host, Port: port, Name: "my-index",
	}}
	r := NewElasticsearch(cfg, discardLogger())
	if err := r.Restore(context.Background(), gzPath); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if bulkHits != 1 {
		t.Fatalf("bulk calls = %d, want 1", bulkHits)
	}
	body := bodies[0]
	for _, want := range []string{
		`"_index":"my-index"`, `"_id":"a"`, `"_id":"b"`, `"k":1`, `"k":2`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("bulk body missing %q; got: %s", want, body)
		}
	}
}

func TestElasticsearch_Restore_BulkErrorsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":true,"items":[{"index":{"status":400,"error":{"type":"mapper_parsing_exception"}}}]}`))
	}))
	t.Cleanup(srv.Close)
	host, port := hostPortFromRestorerURL(t, srv.URL)
	gzPath := writeGzippedBytes(t, "es.ndjson.gz", esNDJSON)
	cfg := &config.Config{DB: config.DB{
		Type: config.DBElasticsearch, Host: host, Port: port, Name: "i",
	}}
	err := NewElasticsearch(cfg, discardLogger()).Restore(context.Background(), gzPath)
	if err == nil || !strings.Contains(err.Error(), "document-level errors") {
		t.Errorf("err = %v", err)
	}
}

func TestElasticsearch_Restore_RequiresIndex(t *testing.T) {
	gzPath := writeGzippedBytes(t, "es.ndjson.gz", esNDJSON)
	cfg := &config.Config{DB: config.DB{Type: config.DBElasticsearch, Host: "x", Port: 9200}}
	err := NewElasticsearch(cfg, discardLogger()).Restore(context.Background(), gzPath)
	if err == nil || !strings.Contains(err.Error(), "DB_NAME") {
		t.Errorf("err = %v", err)
	}
}

func hostPortFromRestorerURL(t *testing.T, s string) (string, int) {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return u.Hostname(), port
}

func writeGzippedBytes(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	if _, err := gw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}
