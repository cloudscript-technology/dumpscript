// Package verifier performs semantic content verification of a produced dump
// artifact. It complements Artifact.Verify (which only checks that the gzip
// envelope is readable) by catching silent corruption: a dump command killed
// mid-stream can still produce a valid gzip file whose SQL/archive content is
// truncated. Each database engine has its own Strategy implementation.
package verifier

import "context"

// Verifier inspects a gzipped dump file and confirms the content is complete.
type Verifier interface {
	Verify(ctx context.Context, gzPath string) error
}
