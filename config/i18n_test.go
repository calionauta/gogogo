package config

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/calionauta/gogogo/internal/projectlocales"
)

// TestLocalesRecordIsFoundFromTheDataDir is the deployment layout: a binary run
// from anywhere with DATA_DIR inside the tree must still read the product's
// declared languages. Reading only "." silently served the template default.
func TestLocalesRecordIsFoundFromTheDataDir(t *testing.T) {
	root := t.TempDir()
	if _, err := projectlocales.Write(root, []string{"en", "fr", "de"}); err != nil {
		t.Fatal(err)
	}
	c := &Config{DataDir: filepath.Join(root, "data")}
	got := c.localesFromRecord()
	if !slices.Contains(got, "fr") || !slices.Contains(got, "de") {
		t.Fatalf("localesFromRecord = %v, want the record next to DATA_DIR", got)
	}
}
