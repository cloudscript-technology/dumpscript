package restorer

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBElasticsearch, func(cfg *config.Config, log *slog.Logger) Restorer {
		return NewElasticsearch(cfg, log)
	})
}

// Elasticsearch reads NDJSON (one document per line, shape
// `{"_id":"...","_source":{...}}`) and replays it via the _bulk API in
// batches. Auth and scheme mirror the dumper.
type Elasticsearch struct {
	cfg  *config.Config
	log  *slog.Logger
	http *http.Client
}

func NewElasticsearch(cfg *config.Config, log *slog.Logger) *Elasticsearch {
	return &Elasticsearch{cfg: cfg, log: log, http: &http.Client{Timeout: 5 * time.Minute}}
}

const esRestoreBatchSize = 500

type esNDLine struct {
	ID     string          `json:"_id"`
	Source json.RawMessage `json:"_source"`
}

func (e *Elasticsearch) Restore(ctx context.Context, gzPath string) error {
	if e.cfg.DB.Name == "" {
		return fmt.Errorf("elasticsearch restore: DB_NAME (index) is required")
	}
	f, err := os.Open(gzPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", gzPath, err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	base, scheme, authHeader := e.urlBase()
	e.log.Info("executing elasticsearch _bulk restore",
		"host", e.cfg.DB.Host, "port", e.cfg.DB.Port, "scheme", scheme,
		"index", e.cfg.DB.Name, "src", gzPath)

	scanner := bufio.NewScanner(gr)
	scanner.Buffer(make([]byte, 1<<16), 16<<20)
	var batch bytes.Buffer
	count, total := 0, 0
	for scanner.Scan() {
		var ln esNDLine
		if err := json.Unmarshal(scanner.Bytes(), &ln); err != nil {
			return fmt.Errorf("parse ndjson line %d: %w", total+1, err)
		}
		fmt.Fprintf(&batch, `{"index":{"_index":%q,"_id":%q}}`+"\n", e.cfg.DB.Name, ln.ID)
		batch.Write(ln.Source)
		batch.WriteByte('\n')
		count++
		total++
		if count >= esRestoreBatchSize {
			if err := e.flushBulk(ctx, base, authHeader, &batch); err != nil {
				return err
			}
			count = 0
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan ndjson: %w", err)
	}
	if batch.Len() > 0 {
		if err := e.flushBulk(ctx, base, authHeader, &batch); err != nil {
			return err
		}
	}
	e.log.Info("elasticsearch restore complete", "docs", total)
	return nil
}

func (e *Elasticsearch) urlBase() (base, scheme, authHeader string) {
	scheme = "http"
	if strings.Contains(e.cfg.DB.DumpOptions, "--scheme=https") {
		scheme = "https"
	}
	for _, tok := range strings.Fields(e.cfg.DB.DumpOptions) {
		if v, ok := strings.CutPrefix(tok, "--auth-header="); ok {
			authHeader = v
		}
	}
	base = fmt.Sprintf("%s://%s:%d", scheme, e.cfg.DB.Host, e.cfg.DB.Port)
	return
}

func (e *Elasticsearch) flushBulk(ctx context.Context, base, authHeader string, batch *bytes.Buffer) error {
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/_bulk", bytes.NewReader(batch.Bytes()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	} else if e.cfg.DB.User != "" || e.cfg.DB.Password != "" {
		req.SetBasicAuth(e.cfg.DB.User, e.cfg.DB.Password)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return fmt.Errorf("_bulk: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("_bulk: %d %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	var parsed struct {
		Errors bool `json:"errors"`
	}
	_ = json.Unmarshal(body, &parsed)
	if parsed.Errors {
		return fmt.Errorf("_bulk reported document-level errors: %s", bytes.TrimSpace(body))
	}
	batch.Reset()
	return nil
}
