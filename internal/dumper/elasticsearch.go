package dumper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBElasticsearch, func(cfg *config.Config, log *slog.Logger) Dumper {
		return NewElasticsearch(cfg, log)
	})
}

// Elasticsearch dumps a single index by walking all documents with the
// scroll API and writing each hit (_id + _source) as one NDJSON line. The
// stream is gzip-compressed into dump_*.ndjson.gz.
//
// Scheme: http unless DUMP_OPTIONS contains --scheme=https.
// Auth:   basic (DB_USER / DB_PASSWORD); for ApiKey/Bearer pass the raw header
//
//	via DUMP_OPTIONS=--auth-header=<raw Authorization header>.
type Elasticsearch struct {
	cfg  *config.Config
	log  *slog.Logger
	http *http.Client
}

func NewElasticsearch(cfg *config.Config, log *slog.Logger) *Elasticsearch {
	return &Elasticsearch{
		cfg:  cfg,
		log:  log,
		http: &http.Client{Timeout: 5 * time.Minute},
	}
}

const (
	esScrollWindow = "1m"
	esPageSize     = 1000
)

type esHit struct {
	ID     string          `json:"_id"`
	Source json.RawMessage `json:"_source"`
}
type esSearchResp struct {
	ScrollID string `json:"_scroll_id"`
	Hits     struct {
		Hits []esHit `json:"hits"`
	} `json:"hits"`
}

func (e *Elasticsearch) Dump(ctx context.Context) (*Artifact, error) {
	if e.cfg.DB.Name == "" {
		return nil, fmt.Errorf("elasticsearch dump: DB_NAME (index) is required")
	}
	const ext = "ndjson"
	out := dumpFilename(e.cfg.WorkDir, ext, time.Now())
	base, scheme, authHeader := e.urlBase()
	e.log.Info("executing elasticsearch scroll dump",
		"host", e.cfg.DB.Host, "port", e.cfg.DB.Port, "scheme", scheme,
		"index", e.cfg.DB.Name, "out", out)

	return runNativeDump(func(w io.Writer) error {
		return e.scrollAll(ctx, base, authHeader, w)
	}, out, ext)
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

func (e *Elasticsearch) scrollAll(ctx context.Context, base, authHeader string, out io.Writer) error {
	index := url.PathEscape(e.cfg.DB.Name)
	body := fmt.Sprintf(`{"size":%d,"query":{"match_all":{}}}`, esPageSize)
	resp, err := e.doJSON(ctx, "POST",
		fmt.Sprintf("%s/%s/_search?scroll=%s", base, index, esScrollWindow),
		authHeader, strings.NewReader(body))
	if err != nil {
		return err
	}

	total := 0
	for {
		parsed, err := parseSearchResp(resp)
		if err != nil {
			return err
		}
		if len(parsed.Hits.Hits) == 0 {
			e.clearScroll(ctx, base, authHeader, parsed.ScrollID)
			e.log.Info("elasticsearch scroll dump complete", "docs", total)
			return nil
		}
		for _, h := range parsed.Hits.Hits {
			line, err := json.Marshal(struct {
				ID     string          `json:"_id"`
				Source json.RawMessage `json:"_source"`
			}{h.ID, h.Source})
			if err != nil {
				return fmt.Errorf("marshal hit %s: %w", h.ID, err)
			}
			if _, err := out.Write(append(line, '\n')); err != nil {
				return fmt.Errorf("write ndjson: %w", err)
			}
			total++
		}
		resp, err = e.doJSON(ctx, "POST", base+"/_search/scroll", authHeader,
			strings.NewReader(fmt.Sprintf(`{"scroll":"%s","scroll_id":%q}`, esScrollWindow, parsed.ScrollID)))
		if err != nil {
			return err
		}
	}
}

func (e *Elasticsearch) clearScroll(ctx context.Context, base, authHeader, scrollID string) {
	if scrollID == "" {
		return
	}
	body := fmt.Sprintf(`{"scroll_id":%q}`, scrollID)
	resp, err := e.doJSON(ctx, "DELETE", base+"/_search/scroll", authHeader, strings.NewReader(body))
	if err != nil {
		e.log.Warn("clear scroll failed", "err", err)
		return
	}
	_, _ = io.Copy(io.Discard, resp)
	_ = resp.Close()
}

func parseSearchResp(rc io.ReadCloser) (*esSearchResp, error) {
	defer rc.Close()
	var out esSearchResp
	if err := json.NewDecoder(rc).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	return &out, nil
}

// doJSON posts JSON and returns the response body on 2xx; errors on non-2xx.
func (e *Elasticsearch) doJSON(ctx context.Context, method, urlStr, authHeader string, body io.Reader) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, method, urlStr, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	} else if e.cfg.DB.User != "" || e.cfg.DB.Password != "" {
		req.SetBasicAuth(e.cfg.DB.User, e.cfg.DB.Password)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, urlStr, err)
	}
	if resp.StatusCode/100 != 2 {
		buf, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("elasticsearch %s %s: %d %s", method, urlStr, resp.StatusCode, bytes.TrimSpace(buf))
	}
	return resp.Body, nil
}
