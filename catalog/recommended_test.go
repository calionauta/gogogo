package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
	"github.com/calionauta/gogogo/internal/recipe"
)

// TestRecommendedListIsValid is the gate for the one file that answers "what
// does the template advise at install": every entry needs an id and a reason,
// ids are unique, and the list is non-empty (an empty list silently turns the
// recommendation off for every operator).
func TestRecommendedListIsValid(t *testing.T) {
	entries, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("recommended list is empty — the scaffold would advise nothing")
	}
	ids, err := IDs()
	if err != nil {
		t.Fatalf("IDs() = %v", err)
	}
	if len(ids) != len(entries) {
		t.Fatalf("IDs() = %v, want one id per entry (%d)", ids, len(entries))
	}
}

// TestRecommendedIDsExist is the typo gate. A recommendation that names an id
// nothing can install is advice nobody can accept: the plan prints it and
// `--install-recommended` would resolve to nothing. Two sets count as known:
// the capabilities this tree ships, and the reviewed catalog entries.
func TestRecommendedIDsExist(t *testing.T) {
	ids, err := IDs()
	if err != nil {
		t.Fatalf("IDs: %v", err)
	}
	known := map[string]bool{}
	for _, c := range capabilities.All {
		known[c.ID] = true
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read catalog dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			known[e.Name()] = true
		}
	}
	for _, id := range ids {
		if !known[id] {
			t.Errorf("%s recommends %q, which is neither a shipped capability nor a catalog entry", RecommendedFile, id)
		}
	}
}

// TestEveryInstalledRecipeIsCategorised is the coverage gate for "2 of 12":
// every receipt in .recipes/ is either a published catalog entry or listed in
// unpublished.yml with a reason. Publishing is a decision, not an accident, and
// reviewing is not the same as shipping in-tree.
func TestEveryInstalledRecipeIsCategorised(t *testing.T) {
	root := ".."
	receipts, err := os.ReadDir(filepath.Join(root, ".recipes"))
	if err != nil {
		t.Fatalf("read .recipes: %v", err)
	}
	published := map[string]bool{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			published[e.Name()] = true
		}
	}
	unpublished, err := LoadUnpublished()
	if err != nil {
		t.Fatalf("LoadUnpublished: %v", err)
	}
	declared := map[string]bool{}
	for _, u := range unpublished {
		declared[u.ID] = true
	}
	for _, r := range receipts {
		if r.IsDir() || filepath.Ext(r.Name()) != ".yml" {
			continue
		}
		id := strings.TrimSuffix(r.Name(), ".yml")
		if published[id] || declared[id] {
			continue
		}
		t.Errorf("%s is installed but neither catalog/%s/ nor %s names it: publish it or write why not",
			id, id, UnpublishedFile)
	}
	for id := range declared {
		if _, err := os.Stat(filepath.Join(root, ".recipes", id+".yml")); err != nil {
			t.Errorf("%s lists %q, which has no receipt (stale decision entry)", UnpublishedFile, id)
		}
		if published[id] {
			t.Errorf("%s lists %q, which is also a catalog entry: pick one", UnpublishedFile, id)
		}
	}
}

// TestReleasedTravelsFromCatalogToReceipt closes the "receipts have no released"
// gap without inventing a date: a bundled receipt for a published version must
// carry that version's reviewed date, so "which release is installed" stays
// answerable from the tree alone. A receipt for an unpublished recipe has no
// date to copy and is left empty on purpose.
func TestReleasedTravelsFromCatalogToReceipt(t *testing.T) {
	root := ".."
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		versions, err := os.ReadDir(filepath.Join(".", id))
		if err != nil {
			t.Fatalf("read catalog/%s: %v", id, err)
		}
		for _, v := range versions {
			if filepath.Ext(v.Name()) != ".yaml" {
				continue
			}
			entry, err := recipe.LoadCatalogEntry(filepath.Join(id, v.Name()))
			if err != nil {
				t.Fatalf("load catalog/%s/%s: %v", id, v.Name(), err)
			}
			receipt, err := recipe.LoadReceipt(root, id)
			if err != nil {
				continue // not installed in this tree: nothing to compare
			}
			if receipt.Version != entry.Version {
				continue // the tree holds another version; publish-time question
			}
			if receipt.Released != entry.Released {
				t.Errorf(".recipes/%s.yml released = %q, catalog says %q: copy the reviewed date, do not invent one",
					id, receipt.Released, entry.Released)
			}
			if receipt.Author.Name != entry.Author.Name {
				t.Errorf(".recipes/%s.yml author = %q, catalog says %q: credit must survive the copy",
					id, receipt.Author.Name, entry.Author.Name)
			}
		}
	}
}
