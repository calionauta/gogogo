// SCOPE:layer=infra,removal=plugin — installer engine: add module-path rebasing
//
// Module-path rebasing, which `gogogo add` runs so a unit copied from the
// template compiles inside the target project: rewriting the template
// module prefix to the target one across the files it just copied.
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rebaseModulePrefix rewrites the template's module path to the target's
// in everything add just brought in. Copied files carry the source tree's
// imports; without this, tidy resolves them against the published module
// (wrong version, wrong path) instead of the local tree.
func rebaseModulePrefix(from, root string, u trimUnit, rc *AddReceipt) error {
	fromMod, err := goModModule(filepath.Join(from, "go.mod"))
	if err != nil {
		return fmt.Errorf("template source go.mod: %w", err)
	}
	toMod, err := goModModule(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("target go.mod: %w", err)
	}
	if fromMod == toMod {
		return nil
	}
	m := u.meta()
	paths := append([]string{}, m.dirs...)
	paths = append(paths, m.files...)
	paths = append(paths, rc.Touched...)
	seen := map[string]bool{}
	rewrote := 0
	for _, p := range paths {
		full, err := joinRoot(root, p)
		if err != nil {
			return err
		}
		if seen[full] {
			continue
		}
		seen[full] = true
		n, err := rebaseTree(full, fromMod, toMod)
		if err != nil {
			return err
		}
		rewrote += n
	}
	if rewrote > 0 {
		rc.Warnings = append(rc.Warnings,
			"rebased "+fromMod+" to "+toMod+" in new and rewired files")
	}
	return nil
}

// goModModule reads the module path from a go.mod file.
func goModModule(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for l := range strings.SplitSeq(string(raw), "\n") {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(l), "module "); ok {
			return strings.TrimSpace(mod), nil
		}
	}
	return "", fmt.Errorf("no module line in %s", path)
}

// rebaseTree rewrites old module prefix to new in .go/.templ files under p.
// Missing paths are errors: rebase runs right after copy, so everything
// listed must exist (unlike strip targets, which sibling trims may own).
func rebaseTree(p, oldMod, newMod string) (int, error) {
	st, err := os.Stat(p)
	if err != nil {
		return 0, fmt.Errorf("rebase missing %s: %w", p, err)
	}
	if !st.IsDir() {
		return rebaseFile(p, oldMod, newMod)
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		m, err := rebaseTree(filepath.Join(p, e.Name()), oldMod, newMod)
		if err != nil {
			return n, err
		}
		n += m
	}
	return n, nil
}

func rebaseFile(p, oldMod, newMod string) (int, error) {
	if !strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, ".templ") {
		return 0, nil
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	if !strings.Contains(string(raw), oldMod) {
		return 0, nil
	}
	//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
	if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(raw), oldMod, newMod)), scaffoldFileMode); err != nil {
		return 0, err
	}
	return 1, nil
}

// joinRoot joins a registry-relative path under root, refusing escapes.
// Registry data is ours, but defense in depth costs one function: a bad
// entry must error, never write outside the checkout.
func joinRoot(root, rel string) (string, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absP, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if absP != absRoot && !strings.HasPrefix(absP, absRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing path escape %q", rel)
	}
	return absP, nil
}
