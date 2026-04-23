package verifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.DBElasticsearch, func(log *slog.Logger) Verifier { return NewElasticsearch(log) })
}

// Elasticsearch verifies a gzipped NDJSON dump by decoding the full gzip
// stream (catches CRC/ISIZE truncation) and ensuring the last non-empty
// line is a valid JSON object. NDJSON has no stable footer token — a
// well-formed terminal record is the completeness signal.
type Elasticsearch struct {
	log *slog.Logger
}

func NewElasticsearch(log *slog.Logger) *Elasticsearch { return &Elasticsearch{log: log} }

func (e *Elasticsearch) Verify(_ context.Context, gzPath string) error {
	tail, err := streamGzipAndTail(gzPath, 4096)
	if err != nil {
		return fmt.Errorf("elasticsearch verify: %w", err)
	}
	if len(tail) == 0 {
		return fmt.Errorf("elasticsearch dump is empty")
	}
	lines := bytes.Split(bytes.TrimRight(tail, "\n"), []byte{'\n'})
	last := lines[len(lines)-1]
	if !json.Valid(last) {
		return fmt.Errorf("elasticsearch dump final NDJSON line is not valid JSON; dump likely truncated")
	}
	e.log.Debug("elasticsearch content verified", "path", gzPath)
	return nil
}
