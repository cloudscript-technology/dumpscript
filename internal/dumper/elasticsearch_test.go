package dumper

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestElasticsearch_Dump_Streaming(t *testing.T) {
	scrollCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/_search") && r.Method == "POST":
			_, _ = w.Write([]byte(`{"_scroll_id":"abc","hits":{"hits":[{"_id":"a","_source":{"k":1}},{"_id":"b","_source":{"k":2}}]}}`))
		case r.URL.Path == "/_search/scroll" && r.Method == "POST":
			scrollCalls++
			if scrollCalls == 1 {
				_, _ = w.Write([]byte(`{"_scroll_id":"abc","hits":{"hits":[{"_id":"c","_source":{"k":3}}]}}`))
				return
			}
			_, _ = w.Write([]byte(`{"_scroll_id":"abc","hits":{"hits":[]}}`))
		case r.URL.Path == "/_search/scroll" && r.Method == "DELETE":
			_, _ = w.Write([]byte(`{"succeeded":true}`))
		default:
			http.Error(w, r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	host, port := hostPortFromURL(t, srv.URL)
	cfg := &config.Config{
		WorkDir: t.TempDir(),
		DB: config.DB{
			Type: config.DBElasticsearch, Host: host, Port: port,
			Name: "my-index",
		},
	}
	d := NewElasticsearch(cfg, discardLogger())
	art, err := d.Dump(context.Background())
	if err != nil {
		t.Fatalf("Dump: %v", err)
	}
	if art.Extension != "ndjson" {
		t.Errorf("extension = %q", art.Extension)
	}
	got := readGzippedLines(t, art.Path)
	if len(got) != 3 {
		t.Fatalf("got %d lines, want 3: %v", len(got), got)
	}
	ids := map[string]bool{}
	for _, ln := range got {
		var x struct {
			ID string `json:"_id"`
		}
		if err := json.Unmarshal([]byte(ln), &x); err != nil {
			t.Fatalf("unmarshal %q: %v", ln, err)
		}
		ids[x.ID] = true
	}
	for _, want := range []string{"a", "b", "c"} {
		if !ids[want] {
			t.Errorf("missing _id=%s in dump", want)
		}
	}
}

func TestElasticsearch_Dump_RequiresIndex(t *testing.T) {
	cfg := &config.Config{DB: config.DB{Type: config.DBElasticsearch, Host: "x", Port: 9200}}
	_, err := NewElasticsearch(cfg, discardLogger()).Dump(context.Background())
	if err == nil || !strings.Contains(err.Error(), "DB_NAME") {
		t.Errorf("err = %v", err)
	}
}

func TestElasticsearch_Dump_PropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	host, port := hostPortFromURL(t, srv.URL)
	cfg := &config.Config{
		WorkDir: t.TempDir(),
		DB:      config.DB{Type: config.DBElasticsearch, Host: host, Port: port, Name: "i"},
	}
	_, err := NewElasticsearch(cfg, discardLogger()).Dump(context.Background())
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
}

func hostPortFromURL(t *testing.T, s string) (string, int) {
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

func readGzippedLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	buf, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
}
