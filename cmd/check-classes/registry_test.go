package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/uiregistry"
)

// The DaisyUI rules have their own suite in internal/daisyui. What stays here is
// what only makes sense for THIS repo: the registry and its doc, and the repo's
// own class vocabulary.

const goodEntry = `{
  "id":"empty-state","title":"Empty state","when":"a list is empty","avoid":"ad-hoc margins",
  "source":"features/x/components/page.templ","shows":["ui-empty"],
  "doc":"docs/ui-compositions.md#1-empty-state"}`

const goodDoc = "# UI compositions\n\n## 1. Empty state\n\nCopy it.\n"

// registryTree builds a fixture with one real source file and one entry.
func registryTree(t *testing.T, entry, doc string) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("ui-empty"),
		"features/x/components/page.templ": `<div class="ui-empty"></div>`,
		"docs/ui-compositions.md":          doc,
		"internal/ui/registry.json":        `{"version":1,"entries":[` + entry + `]}`,
	})
}

// GREEN: an entry that points at real markup and a real anchor passes.
func TestRegistryGoodEntryIsClean(t *testing.T) {
	t.Parallel()
	if got := findingsFor(t, registryTree(t, goodEntry, goodDoc)); got != "" {
		t.Fatalf("expected a clean registry, got:\n%s", got)
	}
}

// RED-proof: the entry claims a class the source no longer contains.
func TestRegistryEntryMustPointAtRealMarkup(t *testing.T) {
	t.Parallel()
	entry := strings.Replace(goodEntry, `"shows":["ui-empty"]`, `"shows":["ui-empty","ui-phantom"]`, 1)
	got := findingsFor(t, registryTree(t, entry, goodDoc))
	if !strings.Contains(got, `"ui-phantom", which no class attribute`) {
		t.Fatalf("expected the phantom class to fail, got:\n%s", got)
	}
}

// A class named only in a COMMENT is not the markup the entry promises.
func TestRegistryClassMustBeInAClassAttribute(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("ui-empty"),
		"features/x/components/page.templ": "// ui-phantom lives only in this comment\n<div class=\"ui-empty\"></div>",
		"docs/ui-compositions.md":          goodDoc,
		"internal/ui/registry.json": `{"version":1,"entries":[{
  "id":"empty-state","title":"Empty state","when":"a list is empty","avoid":"ad-hoc margins",
  "source":"features/x/components/page.templ","shows":["ui-empty","ui-phantom"],
  "doc":"docs/ui-compositions.md#1-empty-state"}]}`,
	})
	if got := findingsFor(t, root); !strings.Contains(got, `"ui-phantom", which no class attribute`) {
		t.Fatalf("expected the comment-only class to fail, got:\n%s", got)
	}
}

// RED-proof: the anchor moved (a renamed composition section).
func TestRegistryDocAnchorMustResolve(t *testing.T) {
	t.Parallel()
	got := findingsFor(t, registryTree(t, goodEntry, "# UI compositions\n\n## 1. Emptiness\n"))
	if !strings.Contains(got, "anchor #1-empty-state does not resolve") {
		t.Fatalf("expected the dangling anchor to fail, got:\n%s", got)
	}
}

// RED-proof, both directions: a composition the doc numbers but the registry
// never lists.
func TestRegistryCoversEveryComposition(t *testing.T) {
	t.Parallel()
	doc := goodDoc + "\n## 2. Field plus its action\n\nJoin them.\n"
	got := findingsFor(t, registryTree(t, goodEntry, doc))
	if !strings.Contains(got, "composition #2-field-plus-its-action") {
		t.Fatalf("expected the undocumented composition to fail, got:\n%s", got)
	}
}

// An entry missing its rule of use is not usable by an agent.
func TestRegistryEntryNeedsItsProse(t *testing.T) {
	t.Parallel()
	entry := strings.Replace(goodEntry, `"avoid":"ad-hoc margins",`, "", 1)
	if got := findingsFor(t, registryTree(t, entry, goodDoc)); !strings.Contains(got, "has no avoid") {
		t.Fatalf("expected the missing field to fail, got:\n%s", got)
	}
}

// The schema version is checked, so an old reader cannot silently parse a new
// format.
func TestRegistryVersionIsChecked(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("ui-empty"),
		"features/x/components/page.templ": `<div class="ui-empty"></div>`,
		"docs/ui-compositions.md":          goodDoc,
		"internal/ui/registry.json":        `{"version":2,"entries":[` + goodEntry + `]}`,
	})
	if got := findingsFor(t, root); !strings.Contains(got, "version is 2") {
		t.Fatalf("expected the version mismatch to fail, got:\n%s", got)
	}
}

// The committed registry must validate, otherwise the gate is decorative.
func TestRealRegistryValidates(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	reg, err := uiregistry.Load(root)
	if err != nil {
		// A trimmed tree removes the shell unit, and the registry with it.
		t.Skipf("no registry in this tree: %v", err)
	}
	if got := registryFindings(root, reg); len(got) > 0 {
		t.Fatalf("the committed registry does not validate:\n%v", got)
	}
}
