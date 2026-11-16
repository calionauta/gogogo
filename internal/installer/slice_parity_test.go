// Parity between the bundled slices.
//
// "Every recipe has the same shape" is not a style preference here: install,
// prune, test-scope and uninstall all read the receipt, so a slice that declares
// less than its neighbours is a slice one of those four gets wrong. The kiosk
// slice is the reference — it is the only demo that is both published (a catalog
// entry) and complete (SQL declared, no production importer outside its shell).
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

// sliceRule is one parity requirement, with the reason a failure matters.
type sliceRule struct {
	name string
	why  string
	ok   func(root string, r recipe.Receipt) (bool, string)
}

var sliceRules = []sliceRule{
	{
		name: "a slice that owns tables declares its SQL",
		why: "uninstall removes the SQL the receipt names and sqlc reads what is copied; " +
			"an undeclared query file survives the removal and fails codegen",
		ok: func(_ string, r recipe.Receipt) (bool, string) {
			if len(r.Declaration.Tables) == 0 {
				return true, ""
			}
			var sql []string
			for path := range r.Files {
				if strings.HasPrefix(path, "internal/db/") {
					sql = append(sql, path)
				}
			}
			if len(sql) == 0 {
				return false, "declares tables but no internal/db file is in the receipt"
			}
			return true, ""
		},
	},
	{
		name: "the receipt names the package that mounts it",
		why:  "the generated registration imports it; an empty Register is a data-only slice and needs a reason",
		ok: func(root string, r recipe.Receipt) (bool, string) {
			if r.Register == "" {
				return false, "no register: this slice mounts nothing"
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(r.Register))); err != nil {
				return false, "register names " + r.Register + ", which is absent"
			}
			return true, ""
		},
	},
}

// TestBundledSlicesHaveTheSameShape runs the rules over every demo slice.
func TestBundledSlicesHaveTheSameShape(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, id := range demoSlices {
		r, err := recipe.LoadReceipt(root, id)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		if r.Removed != "" {
			continue // a dropped slice is a tombstone; nothing to keep in shape
		}
		for _, rule := range sliceRules {
			ok, detail := rule.ok(root, r)
			if ok {
				continue
			}
			t.Errorf("%s: %s (%s): %s", id, rule.name, rule.why, detail)
		}
	}
}

// TestBundledSlicesHaveOneSurface answers the other half of "same pattern": a
// slice mounts through the generated registration, its shell, or the
// composition root — never from shared infrastructure. That import is what made
// `todo` impossible to drop, and cmd/check-product-boundary enforces it on
// production code.
//
// TESTS are held to a slightly looser rule, and the difference is deliberate:
// a test that migrates a schema has to name the slice whose DDL it applies, and
// forbidding that would mean deleting coverage. What is NOT acceptable is a
// test-only import that hides a real dependency, so those are listed here with
// the reason — a new one fails until somebody writes it down.
func TestBundledSlicesHaveOneSurface(t *testing.T) {
	root := filepath.Join("..", "..")
	// Known test-only couplings, each with the reason it cannot be the slice's
	// own package yet.
	allowedTestImporters := map[string]string{
		"features/dashboard/handlers/chi_test.go": "the fixture needs a migrated todo table to read a declared metric",
		"internal/db/store_test.go":               "the generated-query suite exercises the reference slice's SQL",
	}
	for _, id := range demoSlices {
		importers, err := sliceImporters(root, id, nil)
		if err != nil {
			t.Fatalf("sliceImporters(%s): %v", id, err)
		}
		for _, importer := range importers {
			switch {
			case strings.HasPrefix(importer, "cmd/"):
				// A shell renders the slice; that is what a shell is.
			case strings.HasPrefix(importer, "internal/server/"):
				// The composition root names every unit by design.
			case strings.HasSuffix(importer, "_test.go"):
				if _, ok := allowedTestImporters[importer]; !ok {
					t.Errorf("%s is imported by %s (test): add it here with the reason it cannot move, "+
						"or move the import", id, importer)
				}
			default:
				t.Errorf("%s is imported by %s: infrastructure must not name a removable slice", id, importer)
			}
		}
	}
}
