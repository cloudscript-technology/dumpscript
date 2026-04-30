package dumper

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestFileSHA256_MatchesStdLib confirms the helper produces the exact
// hex digest sha256.Sum would. This protects against subtle bugs (e.g. using
// io.Copy with a wrong reader) and makes the contract precise.
func TestFileSHA256_MatchesStdLib(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "a.bin")
	payload := []byte("the quick brown fox jumps over the lazy dog\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(payload)
	if got != hex.EncodeToString(want[:]) {
		t.Fatalf("checksum mismatch:\n got  %s\n want %s", got, hex.EncodeToString(want[:]))
	}
}

func TestFileSHA256_EmptyPath(t *testing.T) {
	got, err := fileSHA256("")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("empty path should return empty checksum, got %q", got)
	}
}

func TestFileSHA256_MissingFile(t *testing.T) {
	if _, err := fileSHA256("/nonexistent/should/fail"); err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}
