// Receipt coverage for the slices a scaffold can drop.
//
// A slice's receipt IS the file set a removal deletes. When the receipt lags the
// tree, the drop leaves source files behind and the product stops compiling —
// exactly what the end-to-end scaffold found with `genui_panel.go`. Generated
// files are exempt: they travel with the source that generates them
// (removeRecipeGenerated deletes them), so listing them would only add churn.
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

func TestDemoReceiptsCoverTheirSlice(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, id := range demoSlices {
		r, err := recipe.LoadReceipt(root, id)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		dir := filepath.Join(root, "features", id)
		// A scaffolded tree drops the demos it does not keep; there is no slice
		// left to cover, and reporting its absence would make every trimmed
		// product fail its own test suite.
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			continue
		}
		walkErr := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if ownErr := assertDeclared(root, id, r, p); ownErr != nil {
				t.Error(ownErr)
			}
			return nil
		})
		if walkErr != nil {
			t.Errorf("%s: %v", id, walkErr)
		}
	}
}

// assertDeclared reports the file a removal would leave behind.
func assertDeclared(root, id string, r recipe.Receipt, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if strings.HasSuffix(rel, "_templ.go") || strings.Contains(rel, "/data/") {
		return nil
	}
	if _, ok := r.Files[rel]; !ok {
		t := &coverageFailure{id: id, file: rel}
		return t
	}
	return nil
}

// coverageFailure is an error that names the fix: the receipt line to add.
type coverageFailure struct {
	id   string
	file string
}

func (c *coverageFailure) Error() string {
	return c.file + " is not in .recipes/" + c.id + ".yml — dropping " + c.id +
		" would leave it behind (add it to the receipt, or generate it)"
}
