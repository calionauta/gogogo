// The tracked language record: what a product ships, and where the answer came
// from.
package projectlocales

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteThenLoadRoundTrips(t *testing.T) {
	root := t.TempDir()
	rec, err := Write(root, []string{"en", "pt-BR", "es"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(rec.Locales) != 3 || rec.Locales[0] != "en" {
		t.Fatalf("recorded %v", rec.Locales)
	}
	got, ok, err := Load(root)
	if err != nil || !ok {
		t.Fatalf("Load: ok=%v err=%v", ok, err)
	}
	if len(got.Locales) != 3 {
		t.Fatalf("loaded %v", got.Locales)
	}
}

// TestWriteForcesEnglishFirst: every product ships English, and the fallback is
// the first entry. A record that starts with pt-BR would make the fallback a
// language most of the world cannot read.
func TestWriteForcesEnglishFirst(t *testing.T) {
	rec, err := Write(t.TempDir(), []string{"pt-BR", "es"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if rec.Locales[0] != "en" {
		t.Fatalf("first locale is %q, want en", rec.Locales[0])
	}
}

func TestWriteRefusesAnEmptySet(t *testing.T) {
	if _, err := Write(t.TempDir(), []string{"  ", ""}); err == nil {
		t.Fatal("an empty language set was accepted")
	}
}

// TestResolvePrecedence is the contract every caller depends on: the record
// answers first, then the deployment's env, then the template default.
func TestResolvePrecedence(t *testing.T) {
	root := t.TempDir()
	// English is added when absent: every product ships it, so the env cannot
	// silently drop the fallback language.
	if tags, source := Resolve(root, "de,fr"); source != "SUPPORTED_LOCALES" ||
		len(tags) != 3 || tags[0] != "en" || tags[1] != "de" {
		t.Fatalf("env fallback = %v from %s", tags, source)
	}
	if tags, source := Resolve(root, ""); source != "default" || tags[0] != "en" {
		t.Fatalf("default = %v from %s", tags, source)
	}
	if _, err := Write(root, []string{"en", "pt-BR", "es"}); err != nil {
		t.Fatal(err)
	}
	if tags, source := Resolve(root, "de,fr"); source != File || len(tags) != 3 {
		t.Fatalf("record must win over env: %v from %s", tags, source)
	}
}

// TestLoadMissingIsNotAnError: a tree that predates the record keeps working.
func TestLoadMissingIsNotAnError(t *testing.T) {
	if _, ok, err := Load(t.TempDir()); ok || err != nil {
		t.Fatalf("missing record: ok=%v err=%v", ok, err)
	}
}

func TestLoadRejectsAMalformedRecord(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, File), []byte("locales: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root); err == nil {
		t.Fatal("a malformed record was accepted")
	}
}
