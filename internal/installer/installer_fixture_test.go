package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// findUnit returns the manifest unit with id, failing the test otherwise.
func findUnit(t *testing.T, id string) trimUnit {
	t.Helper()
	for _, u := range manifestUnits {
		if u.id == id {
			return u
		}
	}
	t.Fatalf("no manifest unit %q", id)
	return trimUnit{}
}
