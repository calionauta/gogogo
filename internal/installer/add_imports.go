// SCOPE:layer=infra,removal=plugin — installer engine: add import restoration
//
// Import restoration: putting back the import lines a trim removed, in
// the right slot and order, and merging a single import into an existing
// parenthesised block.
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// addImports restores import lines the trim dropped: for every import-like
// drop substr, the full line is extracted from source and inserted sorted
// into the target import block. Idempotent.
func addImports(tree *treeFS, from, root string, u trimUnit, rc *AddReceipt) error {
	type target struct {
		path    string
		substrs []string
	}
	var targets []target
	for _, ed := range u.extraDrops {
		var imports []string
		for _, sub := range ed.substrs {
			if strings.HasSuffix(sub, `"`) {
				imports = append(imports, sub)
			}
		}
		if len(imports) > 0 {
			targets = append(targets, target{path: ed.path, substrs: imports})
		}
	}
	if len(u.desktopDropLines) > 0 {
		targets = append(targets, target{
			path:    filepath.Join("cmd", "desktop", "main.go"),
			substrs: u.desktopDropLines,
		})
	}
	for _, t := range targets {
		if _, err := tree.Lstat(filepath.Join(root, filepath.FromSlash(t.path))); os.IsNotExist(err) {
			// Sibling trim deleted the target (e.g. sounds imports in
			// a removed layout): nothing to restore, not an error.
			continue
		}
		n, err := insertImports(tree, from, root, t.path, t.substrs, rc)
		if err != nil {
			return err
		}
		rc.LinesInserted += n
	}
	return nil
}

// insertImports extracts full import lines from source and merges them
// sorted into the target's import block (stdlib group vs external group).
// An import is restored only when the target actually uses the package
// (qualifier appears outside import lines): restoring sounds imports into
// layouts whose call sites stay manual (by design) would break the build
// with an unused import.
func insertImports(tree *treeFS, from, root, path string, substrs []string, rc *AddReceipt) (int, error) {
	srcRaw, err := os.ReadFile(filepath.Join(from, filepath.FromSlash(path)))
	if err != nil {
		return 0, fmt.Errorf("template source missing %s", path)
	}
	dst := filepath.Join(root, filepath.FromSlash(path))
	dstRaw, err := tree.ReadFile(dst)
	if err != nil {
		return 0, fmt.Errorf("target missing %s", path)
	}
	var want []string
	for _, sub := range substrs {
		for l := range strings.SplitSeq(string(srcRaw), "\n") {
			if strings.Contains(l, sub) {
				want = append(want, strings.TrimSpace(l))
				break
			}
		}
	}
	if len(want) == 0 {
		return 0, nil
	}
	lines := strings.Split(string(dstRaw), "\n")
	inserted := 0
	for _, imp := range want {
		if slices.Contains(lines, imp) {
			continue
		}
		if !importUsed(lines, imp) {
			continue
		}
		at := importSlot(lines, imp)
		if at == -1 {
			// No import block (single-line `import "x"` style, as in
			// .templ headers): merge lone imports into one sorted block
			// at the first import's position, else a lone line after
			// the package clause. goimports-clean either way.
			var err error
			lines, err = mergeSingleImports(lines, imp)
			if err != nil {
				return inserted, fmt.Errorf("%s: %w", path, err)
			}
			inserted++
			continue
		}
		lines = append(lines[:at], append([]string{imp}, lines[at:]...)...)
		inserted++
	}
	if inserted == 0 {
		return 0, nil
	}
	if err := tree.WriteFile(dst, []byte(strings.Join(lines, "\n")), scaffoldFileMode); err != nil {
		return inserted, err
	}
	rc.touch(filepath.ToSlash(path))
	return inserted, nil
}

// importBlockOpen is the exact opening line of a Go import block.
const importBlockOpen = "import ("

// importSlot finds the sorted position for imp: stdlib paths (no dot)
// join the first group, everything else the last group. Groups are
// contiguous non-empty line runs inside the import block.
func importSlot(lines []string, imp string) int {
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == importBlockOpen {
			start = i
			break
		}
	}
	if start == -1 {
		return -1
	}
	type span struct{ from, to int }
	var groups []span
	cur := -1
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == ")" {
			break
		}
		if t == "" {
			cur = -1
			continue
		}
		if cur == -1 {
			groups = append(groups, span{from: i, to: i})
			cur = len(groups) - 1
		} else {
			groups[cur].to = i
		}
	}
	if len(groups) == 0 {
		return -1
	}
	quoted := imp
	if i := strings.Index(quoted, `"`); i >= 0 {
		quoted = quoted[i:]
	}
	inner := quoted
	if parts := strings.Split(quoted, `"`); len(parts) > 1 {
		inner = parts[1]
	}
	g := groups[len(groups)-1]
	if !strings.Contains(inner, ".") {
		g = groups[0]
	}
	for i := g.from; i <= g.to; i++ {
		if compareImportLine(lines[i], imp) > 0 {
			return i
		}
	}
	return g.to + 1
}

func compareImportLine(a, b string) int {
	return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
}

// importUsed reports whether the package imported by line is referenced
// outside import lines (qualifier + "."). .templ call sites count:
// @sounds.X contains "sounds.".
func importUsed(lines []string, importLine string) bool {
	qual := importQualifier(importLine)
	if qual == "" {
		return true
	}
	probe := qual + "."
	inBlock := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == importBlockOpen {
			inBlock = true
			continue
		}
		if inBlock && t == ")" {
			inBlock = false
			continue
		}
		if inBlock || (strings.HasPrefix(t, "import ") && !strings.Contains(t, "(")) {
			continue
		}
		if strings.Contains(l, probe) {
			return true
		}
	}
	return false
}

// importQualifier extracts the package qualifier from an import line:
// the alias, or the last path segment.
func importQualifier(importLine string) string {
	t := strings.TrimSpace(importLine)
	t = strings.TrimPrefix(t, "import ")
	t = strings.TrimSpace(t)
	if fields := strings.Fields(t); len(fields) == 2 {
		return strings.Trim(fields[0], "_")
	}
	quoted := t
	if i, j := strings.Index(quoted, `"`), strings.LastIndex(quoted, `"`); i >= 0 && j > i {
		quoted = quoted[i+1 : j]
	}
	if i := strings.LastIndex(quoted, "/"); i >= 0 {
		quoted = quoted[i+1:]
	}
	return quoted
}

// mergeSingleImports merges lone `import "x"` lines plus imp into one
// sorted block at the first import's position (or a lone line after the
// package clause when no imports exist yet).
func mergeSingleImports(lines []string, imp string) ([]string, error) {
	var singles []int
	pkgIdx := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "package ") && pkgIdx == -1 {
			pkgIdx = i
		}
		if strings.HasPrefix(t, `import "`) && strings.HasSuffix(t, `"`) {
			singles = append(singles, i)
		}
	}
	if pkgIdx == -1 {
		return nil, fmt.Errorf("no package clause found")
	}
	paths := []string{imp}
	for _, i := range singles {
		paths = append(paths, strings.TrimSpace(lines[i]))
	}
	// Strip the lone-import keyword: block entries are bare paths.
	for i, p := range paths {
		paths[i] = strings.TrimSpace(strings.TrimPrefix(p, "import "))
	}
	slices.Sort(paths)
	block := append([]string{importBlockOpen}, append(tabIndent(paths), ")")...)
	if len(singles) == 0 {
		// No imports yet: a lone single-line import is the smallest
		// diff (imp arrives as a full `import "…"` line).
		at := pkgIdx + 1
		return append(lines[:at], append([]string{imp}, lines[at:]...)...), nil
	}
	// Replace the first single import with the block, drop the rest.
	kept := lines[:0]
	for i, l := range lines {
		if slices.Contains(singles[1:], i) {
			continue
		}
		if i == singles[0] {
			kept = append(kept, block...)
			continue
		}
		kept = append(kept, l)
	}
	return kept, nil
}

func tabIndent(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, "\t"+l)
	}
	return out
}
