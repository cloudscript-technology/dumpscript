package dumper

import (
	"os"
	"strings"
	"testing"
)

// TestWriteMongoCredsFile_WritesYamlAndCleansUp confirms the helper writes a
// 0600 YAML file containing the password keyed as `password:` (per
// mongo-tools 100.7+ format), and that the cleanup function removes it.
func TestWriteMongoCredsFile_WritesYamlAndCleansUp(t *testing.T) {
	path, cleanup, err := writeMongoCredsFile("s3cret-with-special chars!@#")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("path should be non-empty when password is set")
	}
	defer cleanup()

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Errorf("perms = %o, want 0600", mode)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// %q in fmt.Fprintf wraps in quotes which is what we want — YAML accepts
	// quoted scalar values safely.
	wantContains := []string{"password:", "s3cret-with-special chars!@#"}
	for _, w := range wantContains {
		if !strings.Contains(string(body), w) {
			t.Errorf("creds file missing %q\nbody:\n%s", w, body)
		}
	}

	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("cleanup did not remove file: err=%v", err)
	}
}

// TestWriteMongoCredsFile_EmptyPasswordReturnsEmptyPath ensures we never
// write an empty creds file just to satisfy the API.
func TestWriteMongoCredsFile_EmptyPasswordReturnsEmptyPath(t *testing.T) {
	path, cleanup, err := writeMongoCredsFile("")
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Fatalf("path = %q, want empty for blank password", path)
	}
	// noop cleanup must not panic
	cleanup()
}
