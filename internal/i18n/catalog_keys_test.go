// The catalog contract: every key the markup asks for exists in every shipped
// language.
//
// A missing key does not fail — Lookup falls back to English, which is the
// right behaviour at runtime and the reason the gap is invisible. The first
// Portuguese user sees an English word in the middle of a translated page and
// nothing reports it. This test reads the keys out of the source and checks
// both catalogs, so "add a string" means "add it twice".
//
// It is deliberately string-based (no templ parsing): the keys are literal
// first arguments to i18n.T / TCount, and a key built at runtime would not be
// translatable anyway — flagging it here is the correct outcome.
package i18n_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/i18n"
)

// keyRef matches i18n.T(ctx, "key"…) and i18n.TCount(ctx, "key"…).
var keyRef = regexp.MustCompile(`i18n\.T(?:Count)?\(ctx,\s*"([^"]+)"`)

// moduleLabelRef matches ui.ModuleLabel, whose key is "nav.<module id>".
var moduleLabelRef = regexp.MustCompile(`ModuleLabel\(ctx,\s*([A-Za-z_][A-Za-z0-9_]*)`)

// sourceRoot is the repo root, relative to internal/i18n.
const sourceRoot = "../.."

// scanKeys collects every catalog key the markup asks for.
func scanKeys(t *testing.T) map[string]string {
	t.Helper()
	keys := map[string]string{}
	roots := []string{"features", "internal/ui"}
	for _, root := range roots {
		err := filepath.WalkDir(filepath.Join(sourceRoot, root), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".templ") {
				return nil
			}
			raw, rerr := os.ReadFile(path) //nolint:gosec // repo-relative literal walk
			if rerr != nil {
				return rerr
			}
			body := string(raw)
			for _, m := range keyRef.FindAllStringSubmatch(body, -1) {
				keys[m[1]] = path
			}
			// ModuleLabel keys are the destination ids; the id→key mapping is
			// fixed, so the literal id is enough to expect a nav row.
			for _, m := range moduleLabelRef.FindAllStringSubmatch(body, -1) {
				// The helper's own body calls ModuleLabel(ctx, m) with a
				// parameter, not a destination; only a literal id names a key.
				if m[1] == "m" {
					continue
				}
				keys["nav."+m[1]] = path
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return keys
}

// TestEveryUsedKeyExistsInEveryLanguage is the gate. It names the file that
// asks for the key, because "missing key" without a location is a hunt.
func TestEveryUsedKeyExistsInEveryLanguage(t *testing.T) {
	cat := i18n.Default()
	keys := scanKeys(t)
	if len(keys) == 0 {
		t.Fatal("scanned no keys; the matcher or the paths are wrong")
	}
	// A module id in ModuleLabel is dynamic at the call site, so the expected
	// key for ModuleInbox ("notifications") and friends is derived here rather
	// than from the variable name.
	var missing []string
	for key, file := range keys {
		for _, tag := range cat.Supported() {
			if _, ok := cat.Lookup(tag, key); !ok {
				missing = append(missing, tag+" "+key+" ("+file+")")
			}
		}
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("missing catalog key: %s", m)
	}
}

// TestCatalogFilesAgree catches the other direction: a key that exists in one
// file and not the other would pass Lookup (English fallback) while the
// translated page silently keeps an English word.
func TestCatalogFilesAgree(t *testing.T) {
	cat := i18n.Default()
	langs := cat.Supported()
	if len(langs) < 2 {
		t.Skip("fewer than two shipped languages")
	}
	perLang := make([]map[string]bool, len(langs))
	for i, tag := range langs {
		perLang[i] = map[string]bool{}
		raw, err := os.ReadFile(filepath.Join("locale", tag+".json"))
		if err != nil {
			t.Fatalf("read %s.json: %v", tag, err)
		}
		for _, key := range jsonKeys(t, raw) {
			perLang[i][key] = true
		}
	}
	base := perLang[0]
	for i := 1; i < len(langs); i++ {
		for key := range base {
			if !perLang[i][key] {
				t.Errorf("%s.json is missing %q (present in %s.json)", langs[i], key, langs[0])
			}
		}
		for key := range perLang[i] {
			if !base[key] {
				t.Errorf("%s.json is missing %q (present in %s.json)", langs[0], key, langs[i])
			}
		}
	}
}

// jsonKeys lists the top-level keys of a catalog file.
func jsonKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
