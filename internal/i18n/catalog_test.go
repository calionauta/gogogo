package i18n

import (
	"embed"
	"testing"
)

//go:embed locale/en.json locale/pt-BR.json
var testFS embed.FS

func loadTestCatalog(t *testing.T) *Catalog {
	t.Helper()
	c, err := LoadFS(testFS, []string{"en", "pt-BR"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoadFSRejectsEmptySupported(t *testing.T) {
	if _, err := LoadFS(testFS, nil, "en"); err == nil {
		t.Fatal("empty supported must fail")
	}
}

func TestLookupFallsBackToDefault(t *testing.T) {
	c := loadTestCatalog(t)
	got, ok := c.Lookup("pt-BR", "nav.modules")
	if !ok || got != "Módulos" {
		t.Fatalf("pt-BR lookup = %q, %v", got, ok)
	}
	got, ok = c.Lookup("de", "nav.modules")
	if !ok || got != "Modules" {
		t.Fatalf("unknown locale must fall back, got %q, %v", got, ok)
	}
	if _, ok := c.Lookup("en", "no.such.key"); ok {
		t.Fatal("unknown key must miss in both")
	}
}

func TestSupportedReturnsCopy(t *testing.T) {
	c := loadTestCatalog(t)
	sup := c.Supported()
	sup[0] = "mutated"
	if c.Supported()[0] != "en" {
		t.Fatal("Supported must return a copy")
	}
}
