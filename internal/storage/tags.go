package storage

import (
	"net/url"
	"sort"
	"strings"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// backupTags returns a small set of stable, low-cardinality tags attached
// to every uploaded backup object. They make S3/GCS/Azure inventory
// queryable by engine/periodicity for cost analysis and orphan detection
// without requiring a separate catalog.
//
// Keys are alphanumeric + underscore (compatible with all three backends);
// values are URL-encoded by the caller as needed.
func backupTags(cfg *config.Config) map[string]string {
	tags := map[string]string{
		"managed_by": "dumpscript",
	}
	if cfg.DB.Type != "" {
		tags["engine"] = string(cfg.DB.Type)
	}
	if cfg.Periodicity != "" {
		tags["periodicity"] = string(cfg.Periodicity)
	}
	return tags
}

// encodeS3Tagging renders a tag map into the URL-encoded form S3 expects on
// PutObject's Tagging field (e.g. "engine=postgres&periodicity=daily").
func encodeS3Tagging(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic for tests + diffs
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(tags[k]))
	}
	return b.String()
}

// metadataPtrMap converts a string-string tag map to the *string-valued map
// required by the Azure SDK.
func metadataPtrMap(tags map[string]string) map[string]*string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]*string, len(tags))
	for k, v := range tags {
		v := v // capture
		out[k] = &v
	}
	return out
}
