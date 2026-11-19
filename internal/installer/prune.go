// Installer: dropping the demo slices a fresh product does not need.
package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/calionauta/gogogo/internal/recipe"
)

// pruneReferenceRecipes removes the demo slices the plan chose, BEFORE the
// rename: the removal has to see the receipts' hashes still matching the tree
// (rename rewrites every file), and a removed slice's dangling wiring has to
// fail here rather than in the user's first `make build`.
//
// It is NOT `recipe uninstall`. Uninstall keeps files the user edited; here
// nobody has edited anything yet, and a receipt's drift describes the
// TEMPLATE's development, not the operator's work. Keeping those files leaves a
// product importing packages that no longer exist, so this path removes the
// whole declared set. The callers gate on sliceImporters first, so the set it
// removes is one nothing kept still imports.
func pruneReferenceRecipes(opt options, drop []string, stdout io.Writer) error {
	if len(drop) == 0 {
		return nil
	}
	for _, id := range drop {
		if err := pruneOneRecipe(opt.dir, id, drop, stdout); err != nil {
			return err
		}
	}
	return nil
}

// pruneOneRecipe removes one slice and every derived surface it owned (policy
// rows, nav entry, domain card, generated registration).
func pruneOneRecipe(root, id string, drop []string, stdout io.Writer) error {
	if _, err := os.Stat(filepath.Join(root, recipe.ReceiptPath(id))); os.IsNotExist(err) {
		// No receipt means no installed slice to uninstall.
		return nil
	}
	// One confined handle for every write below: os.Root refuses a path that
	// would leave the target even through a planted symlink, which a lexical
	// join cannot see. The tree is opened per recipe so a corrupt checkout
	// fails before any file is touched.
	tree, treeErr := openTree(root)
	if treeErr != nil {
		return fmt.Errorf("scaffold: %w", treeErr)
	}
	defer func() { _ = tree.Close() }()
	r, err := recipe.LoadReceipt(root, id)
	if err != nil {
		return fmt.Errorf("scaffold: reading receipt for %s: %w", id, err)
	}
	if r.Removed != "" {
		return nil
	}
	res, err := recipe.RemoveAll(root, r)
	if err != nil {
		return fmt.Errorf("scaffold: removing %s: %w", id, err)
	}
	// The receipt names the slice's OWN files; the shared lines that wire it
	// into the server live in the unit's declaration, which no receipt records.
	// Applying it here is what lets the package it fed actually go away.
	if u, ok := builtinUnitByID(id); ok {
		if err := applyDropEdits(tree, u); err != nil {
			return fmt.Errorf("scaffold: removing wiring of %s: %w", id, err)
		}
		// The unit's other CAPABILITIES go with it: unitCaps says the whiteboard
		// unit owns {whiteboard, collab, notes}, so a dropped bundle takes the
		// transport it depends on — the registry's own promise ("removing the
		// whiteboard bundle removes collab"), executed instead of described.
		// Their shared wiring is the same server seam, edited once above.
		//
		// A capability is only removed when NOTHING kept still imports it: a
		// scaffold that drops `notes` and `whiteboard` (shape api does) leaves
		// collab orphaned and it goes; one that keeps `notes` still needs the
		// transport, and removeUnitCapabilities leaves it alone.
		if err := removeUnitCapabilities(tree, u, drop); err != nil {
			return fmt.Errorf("scaffold: removing capabilities of %s: %w", id, err)
		}
	}
	if _, err := removeDerived(root, id, res.Removed, r); err != nil {
		return fmt.Errorf("scaffold: removing derived surfaces of %s: %w", id, err)
	}
	// The receipt stays, marked removed: regenerateBundledUnits reads that flag
	// to keep the unit out of units.gen.go, and the file records who owned the
	// data the drop left behind (migrations are append-only).
	if err := r.MarkRemoved(time.Now()); err != nil {
		return fmt.Errorf("scaffold: marking %s removed: %w", id, err)
	}
	if err := recipe.SaveReceipt(root, r); err != nil {
		return fmt.Errorf("scaffold: writing receipt for %s: %w", id, err)
	}
	if err := regenerateRegistration(root); err != nil {
		return err
	}
	if err := regenerateBundledUnits(root); err != nil {
		return err
	}
	removeEmptyFeatureDirs(root, id)
	if stdout != nil {
		fmt.Fprintf(stdout, "scaffold: dropped %s (%d files); read it again in the template "+
			"with `recipe source %s`\n", id, len(res.Removed), id)
	}
	return nil
}

// removeEmptyFeatureDirs deletes the directories a removed slice leaves behind.
// Only EMPTY directories are removed, deepest first, so a file the slice did not
// declare is never touched.
func removeEmptyFeatureDirs(root, id string) {
	base := filepath.Join(root, "features", id)
	var dirs []string
	_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			dirs = append(dirs, p)
		}
		return nil
	})
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, dir := range dirs {
		if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
}
