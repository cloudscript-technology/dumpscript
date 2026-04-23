package storage

import (
	"fmt"
	"strconv"
	"strings"
)

// parseSize converts "100M", "1G", "512K", "1024" etc. into bytes.
// Accepts suffixes K, M, G, T (case-insensitive, binary units: 1K = 1024).
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	var mult int64 = 1
	switch s[len(s)-1] {
	case 'K':
		mult = 1 << 10
		s = s[:len(s)-1]
	case 'M':
		mult = 1 << 20
		s = s[:len(s)-1]
	case 'G':
		mult = 1 << 30
		s = s[:len(s)-1]
	case 'T':
		mult = 1 << 40
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", s, err)
	}
	return n * mult, nil
}
