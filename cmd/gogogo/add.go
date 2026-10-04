package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// Shared file paths for add/trim mechanics (goconst-quiet, single spelling).
const (
	todoGoFile   = "features/todo/handlers/todo.go"
	todoRepoFile = "features/todo/handlers/todo_repo.go"
	dirMode      = 0o755
)

// Insertion anchors for add: where extracted spans and call lines land in
// the target checkout. Keyed by file path (convention over configuration):
// scaffolded trees keep these shapes, anything else fails fast.
const (
	// routerCallsAnchor ends the OnServe closure; capability calls land
	// directly above it, in manifest order.
	routerCallsAnchor = "\t\treturn se.Next()"
	// mainCallsAnchor is the shutdown defer; dagnats boot lines land below.
	mainCallsAnchor = "\tdefer shutdown()"
	// mainBlankUse is removed when dagnats comes back (todoH used again).
	mainBlankUse = "\t_ = todoH // dagnats removed: handler stays wired via router"
	// desktopBlockAnchor opens the Wails boot section; the Phase C demo
	// block lands above it.
	desktopBlockAnchor = "\taddr := fmt.Sprintf(\"%s:%d\", cfg.Host, cfg.Port)"
	// navLinkAnchorSubstr marks the Todo nav link; section links land below.
	navLinkAnchorSubstr = "}>Todo</a>"
	// todoLayoutAnchor is the DaisyUI fallback; skin dispatches land above.
	todoLayoutAnchor = "\treturn components.Layout("
	// todoRegionAnchor is the region fallback; skin cases land above.
	todoRegionAnchor = "\tdefault:"
)

// AddReceipt records what one add run actually changed. Touched lists
// every file written, so agents (and review) can scope the diff.
type AddReceipt struct {
	ID            string   `json:"id"`
	DirsCopied    int      `json:"dirsCopied"`
	FilesCopied   int      `json:"filesCopied"`
	LinesInserted int      `json:"linesInserted"`
	Warnings      []string `json:"warnings"`
	Touched       []string `json:"touchedFiles"`
}

// addUnit copies one registry unit from a pristine template checkout into
// an existing project and rewires it, proving with tidy+build. It is the
// inverse of trim: spans are extracted from the source tree with the same
// markers trim strips by, then inserted at the anchor table above.
// .templ call sites and the navbar brand stay manual (warned): their
// positions vary per layout and a wrong guess is worse than guidance.
func addUnit(from, root string, u trimUnit, rc *AddReceipt) error {
	m := u.meta()
	if err := requireScaffold(root); err != nil {
		return err
	}
	if err := copyUnitPaths(from, root, m, rc); err != nil {
		return err
	}
	if err := addRouterCalls(root, u, rc); err != nil {
		return err
	}
	if err := addSpans(from, root, u, rc); err != nil {
		return err
	}
	if err := addImports(from, root, u, rc); err != nil {
		return err
	}
	if u.id == unitDagnats {
		mp := filepath.Join(root, "cmd", "web", "main.go")
		if err := dropLineContaining(mp, mainBlankUse); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := rebaseModulePrefix(from, root, u, rc); err != nil {
		return err
	}
	rc.Warnings = append(rc.Warnings, addFollowUps(u)...)
	return nil
}

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

// copyUnitPaths copies missing owned dirs/files from the template tree.
// Present paths are skipped (idempotent re-add).
func copyUnitPaths(from, root string, m unitMeta, rc *AddReceipt) error {
	for _, d := range m.dirs {
		dst, err := joinRoot(root, d)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		if err := copyPath(filepath.Join(from, filepath.FromSlash(d)), dst); err != nil {
			return fmt.Errorf("copy %s: %w", d, err)
		}
		rc.DirsCopied++
	}
	for _, f := range m.files {
		dst, err := joinRoot(root, f)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		if err := copyPath(filepath.Join(from, filepath.FromSlash(f)), dst); err != nil {
			return fmt.Errorf("copy %s: %w", f, err)
		}
		rc.FilesCopied++
	}
	return nil
}

// requireScaffold fails fast on trees the installer cannot rewire:
// without the flat Init list, insertion anchors mean nothing.
func requireScaffold(root string) error {
	for _, f := range []string{
		filepath.Join("router", "router.go"),
		"go.mod",
	} {
		p, err := joinRoot(root, f)
		if err != nil {
			return &exitError{code: 1, msg: err.Error()}
		}
		//nolint:gosec // G703 path validated by joinRoot above (no escape).
		if _, err := os.Stat(p); err != nil {
			return &exitError{code: 1, msg: "not a gogogo-scaffolded checkout (missing " + f + ")"}
		}
	}
	return nil
}

// touch records a written file for the rebase pass and the receipt.
func (rc *AddReceipt) touch(path string) {
	if slices.Contains(rc.Touched, path) {
		return
	}
	rc.Touched = append(rc.Touched, path)
}

// addRouterCalls inserts the unit's call lines above the OnServe return.
// Idempotent: present lines are skipped.
func addRouterCalls(root string, u trimUnit, rc *AddReceipt) error {
	calls := routerCallLines(u)
	if len(calls) == 0 {
		return nil
	}
	p := filepath.Join(root, "router", "router.go")
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	anchor := -1
	for i, l := range lines {
		if l == routerCallsAnchor {
			anchor = i
			break
		}
	}
	if anchor == -1 {
		return &exitError{code: 1, msg: "cannot locate insertion anchor in router/router.go (not a scaffolded Init?)"}
	}
	var insert []string
	for _, c := range calls {
		dup := false
		for _, l := range lines {
			if strings.TrimSpace(l) == strings.TrimSpace(c) {
				dup = true
				break
			}
		}
		if !dup {
			insert = append(insert, "\t\t"+c)
		}
	}
	if len(insert) == 0 {
		return nil
	}
	lines = append(lines[:anchor], append(insert, lines[anchor:]...)...)
	rc.LinesInserted += len(insert)
	rc.touch("router/router.go")
	//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
	return os.WriteFile(p, []byte(strings.Join(lines, "\n")), scaffoldFileMode)
}

// routerCallLines are the exact Init call lines a unit owns: explicit
// routerCalls plus router.go line-drops that are calls, not imports.
// Import-lookalikes (ending in a quote) are restored by addImports instead.
func routerCallLines(u trimUnit) []string {
	var out []string
	seen := map[string]bool{}
	add := func(c string) {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	for _, ed := range u.extraDrops {
		if ed.path != routerGoFile {
			continue
		}
		for _, sub := range ed.substrs {
			if strings.HasSuffix(sub, `"`) {
				continue
			}
			add(sub)
		}
	}
	return out
}

// addSpans extracts each strip span from the source tree and inserts it at
// its anchor. Anchors resolve per rule (addBefore/addAfter win) with path
// defaults: main.go after the shutdown defer, desktop before the Wails boot
// section, .templ/handler files from templSpanAnchor.
func addSpans(from, root string, u trimUnit, rc *AddReceipt) error {
	type item struct {
		path   string
		rule   stripRule
		anchor spanAnchor
	}
	pathDefault := func(path string) spanAnchor {
		switch path {
		case filepath.Join("cmd", "web", "main.go"):
			return anchorAfter(mainCallsAnchor)
		case filepath.Join("cmd", "desktop", "main.go"):
			return anchorBefore(desktopBlockAnchor)
		default:
			if anchor, ok := templSpanAnchor(path); ok {
				return anchor
			}
			return spanAnchor{}
		}
	}
	var items []item
	collect := func(path string, rules []stripRule) {
		def := pathDefault(path)
		for _, r := range rules {
			anchor := def
			if r.addBefore != "" || r.addAfter != "" {
				anchor = spanAnchor{before: r.addBefore, after: r.addAfter}
			}
			items = append(items, item{path: path, rule: r, anchor: anchor})
		}
	}
	collect(filepath.Join("cmd", "web", "main.go"), u.mainStrips)
	collect(filepath.Join("cmd", "desktop", "main.go"), u.desktopStrips)
	for _, es := range u.extraStrips {
		collect(es.path, es.rules)
	}
	for _, it := range items {
		if it.anchor == (spanAnchor{}) {
			return fmt.Errorf("%s: no insertion anchor (add convention covers listed paths only)", it.path)
		}
		dst := filepath.Join(root, filepath.FromSlash(it.path))
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			// Sibling trim deleted the target (e.g. sounds call sites
			// in a removed layout): nothing to restore, not an error.
			continue
		}
		n, err := insertRuleSpan(from, root, it.path, it.rule, it.anchor, rc)
		if err != nil {
			return err
		}
		rc.LinesInserted += n
	}
	return nil
}

// insertRuleSpan copies one rule's span from source into target at anchor.
func insertRuleSpan(from, root, path string, r stripRule, anchor spanAnchor, rc *AddReceipt) (int, error) {
	return insertSpan(from, root, path, path, []stripRule{r}, anchor, rc)
}

// spanAnchor locates an insertion point: before/after the first line
// exactly equal (or, for navLinkAnchor, containing) the marker.
type spanAnchor struct {
	before string
	after  string
	// afterContains matches by substring (nav links carry classes).
	afterContains string
}

func anchorBefore(line string) spanAnchor { return spanAnchor{before: line} }
func anchorAfter(line string) spanAnchor  { return spanAnchor{after: line} }

// templSpanAnchor maps edited .templ/navbar/handler files to anchors.
// Files without an entry (sounds call sites, brand) stay manual by design.
func templSpanAnchor(path string) (spanAnchor, bool) {
	switch path {
	case navbarTempl:
		return spanAnchor{afterContains: navLinkAnchorSubstr}, true
	case "features/todo/handlers/todo.go":
		return anchorBefore(todoLayoutAnchor), true
	case "features/todo/handlers/todo_repo.go":
		return anchorBefore(todoRegionAnchor), true
	default:
		return spanAnchor{}, false
	}
}

// insertSpan copies the lines covered by rules from the source file into
// the target file at anchor. Fully present spans are skipped (idempotent).
func insertSpan(
	from, root, srcPath, dstPath string,
	rules []stripRule, anchor spanAnchor, rc *AddReceipt,
) (int, error) {
	srcRaw, err := os.ReadFile(filepath.Join(from, filepath.FromSlash(srcPath)))
	if err != nil {
		return 0, fmt.Errorf("template source missing %s (pass --from a pristine checkout)", srcPath)
	}
	srcLines := strings.Split(string(srcRaw), "\n")
	dst := filepath.Join(root, filepath.FromSlash(dstPath))
	dstRaw, err := os.ReadFile(dst)
	if err != nil {
		return 0, fmt.Errorf("target missing %s (not a scaffolded checkout?)", dstPath)
	}
	dstLines := strings.Split(string(dstRaw), "\n")
	inserted := 0
	for _, r := range rules {
		span := extractSpan(srcLines, r)
		if len(span) == 0 {
			return inserted, fmt.Errorf("template source %s lost marker %q (drifted template?)", srcPath, r.startMarker)
		}
		if spanPresent(dstLines, span) {
			continue
		}
		at, err := findAnchor(dstLines, anchor)
		if err != nil {
			return inserted, fmt.Errorf("%s: %w", dstPath, err)
		}
		dstLines = append(dstLines[:at], append(slices.Clone(span), dstLines[at:]...)...)
		inserted += len(span)
	}
	if inserted == 0 {
		return 0, nil
	}
	//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
	if err := os.WriteFile(dst, []byte(strings.Join(dstLines, "\n")), scaffoldFileMode); err != nil {
		return inserted, err
	}
	rc.touch(filepath.ToSlash(dstPath))
	return inserted, nil
}

// extractSpan returns the source lines a strip rule would delete.
func extractSpan(srcLines []string, r stripRule) []string {
	for i := range len(srcLines) {
		if !strings.Contains(srcLines[i], r.startMarker) {
			continue
		}
		end := stripEnd(srcLines, i, r)
		if end == -1 {
			return nil
		}
		return slices.Clone(srcLines[i : end+1])
	}
	return nil
}

// spanPresent reports whether every span line already exists in target.
func spanPresent(dstLines, span []string) bool {
	set := make(map[string]bool, len(dstLines))
	for _, l := range dstLines {
		set[l] = true
	}
	for _, l := range span {
		if !set[l] {
			return false
		}
	}
	return true
}

func findAnchor(dstLines []string, anchor spanAnchor) (int, error) {
	for i, l := range dstLines {
		switch {
		case anchor.before != "" && l == anchor.before:
			return i, nil
		case anchor.after != "" && l == anchor.after:
			return i + 1, nil
		case anchor.afterContains != "" && strings.Contains(l, anchor.afterContains):
			return i + 1, nil
		}
	}
	return -1, fmt.Errorf("cannot locate insertion anchor (evolved file?)")
}

// addImports restores import lines the trim dropped: for every import-like
// drop substr, the full line is extracted from source and inserted sorted
// into the target import block. Idempotent.
func addImports(from, root string, u trimUnit, rc *AddReceipt) error {
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
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(t.path))); os.IsNotExist(err) {
			// Sibling trim deleted the target (e.g. sounds imports in
			// a removed layout): nothing to restore, not an error.
			continue
		}
		n, err := insertImports(from, root, t.path, t.substrs, rc)
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
func insertImports(from, root, path string, substrs []string, rc *AddReceipt) (int, error) {
	srcRaw, err := os.ReadFile(filepath.Join(from, filepath.FromSlash(path)))
	if err != nil {
		return 0, fmt.Errorf("template source missing %s", path)
	}
	dst := filepath.Join(root, filepath.FromSlash(path))
	dstRaw, err := os.ReadFile(dst)
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
	//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
	if err := os.WriteFile(dst, []byte(strings.Join(lines, "\n")), scaffoldFileMode); err != nil {
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

// copyPath copies a file or directory tree, creating parents.
func copyPath(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		raw, readErr := os.ReadFile(src)
		if readErr != nil {
			return readErr
		}
		if mkErr := os.MkdirAll(filepath.Dir(dst), dirMode); mkErr != nil {
			return mkErr
		}
		//nolint:gosec // G306 scaffolded repo files are 0644 tracked sources, same as a git checkout.
		return os.WriteFile(dst, raw, scaffoldFileMode)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, dirMode); err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// addFollowUps lists the manual steps add cannot do: .templ call sites
// (positions vary per layout) and the navbar brand (user taste).
func addFollowUps(u trimUnit) []string {
	var out []string
	switch u.id {
	case unitSounds:
		out = append(out,
			"sounds has no UI until call sites return: re-add "+
				"@sounds.SoundAssets() to page layouts and "+
				"@sounds.SoundToggle() to the navbar, then `make templ` "+
				"(checklist in features/sounds/sounds.go).")
	case unitLanding:
		out = append(out,
			"navbar brand still points at /todo: point it back at / "+
				"if the landing page is the front door again.")
	}
	return out
}

// addEnvelope is the single machine-readable document for add runs.
type addEnvelope struct {
	Unit     string       `json:"unit"`
	From     string       `json:"from"`
	Dir      string       `json:"dir"`
	Receipts []AddReceipt `json:"receipts"`
	BuildOk  bool         `json:"buildOk"`
}

// addClosure orders unit + its transitive deps (deps first). Soft edges
// like landing→sounds exist because copied .templ files import the
// package; trim handles the reverse with strips, add needs the package
// present. Units without an installer entry fail fast.
func addClosure(unitID string) ([]trimUnit, error) {
	byUnit := map[string]trimUnit{}
	for _, u := range manifestUnits {
		byUnit[u.id] = u
	}
	byCap := capabilities.ByID()
	unitOf := map[string]string{}
	for _, u := range manifestUnits {
		for _, c := range u.caps() {
			unitOf[c] = u.id
		}
	}
	var ordered []trimUnit
	visited := map[string]bool{}
	var visit func(id string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true
		u, ok := byUnit[id]
		if !ok {
			return fmt.Errorf("unknown unit %q (valid: %s)", id, strings.Join(dropIDs(manifestUnits), ", "))
		}
		for _, dep := range u.caps() {
			c := byCap[dep]
			for _, d := range c.DependsOn {
				du, ok := unitOf[d]
				if !ok {
					return fmt.Errorf("capability %q needs %q, which no installer unit provides", dep, d)
				}
				if err := visit(du); err != nil {
					return err
				}
			}
		}
		ordered = append(ordered, u)
		return nil
	}
	if err := visit(unitID); err != nil {
		return nil, err
	}
	return ordered, nil
}

// addOptions are the parsed `gogogo add` flags plus resolved units.
type addOptions struct {
	units  []trimUnit
	unitID string
	from   string
	dir    string
	yes    bool
	dryRun bool
	format string
}

// runAdd implements `gogogo add <unit> --from TEMPLATE --dir PROJECT`:
// the inverse of trim for existing checkouts (scaffolded or evolved).
func runAdd(args []string, stdin *os.File, stdout *os.File) error {
	opt, err := parseAddArgs(args, stdout)
	if err != nil {
		return err
	}
	if opt.format == planFormatJSON {
		// Single JSON document per invocation: plan on dry-run,
		// envelope on apply.
		if opt.dryRun {
			for _, u := range opt.units {
				printAddPlan(stdout, opt.format, u, opt.from, opt.dir)
			}
			return nil
		}
	} else {
		for _, u := range opt.units {
			printAddPlan(stdout, opt.format, u, opt.from, opt.dir)
		}
		if opt.dryRun {
			return nil
		}
	}
	return executeAdd(opt, stdin, stdout)
}

// executeAdd applies units, proves, and reports (text plan already shown,
// JSON envelope here).
func executeAdd(opt addOptions, stdin *os.File, stdout *os.File) error {
	if err := requireScaffold(opt.dir); err != nil {
		return err
	}
	if !opt.yes && !confirmAdd(stdout, stdin, opt.unitID) {
		fmt.Fprintln(stdout, "gogogo: aborted — nothing changed (re-run with --yes to skip this prompt)")
		return nil
	}
	var receipts []AddReceipt
	for _, u := range opt.units {
		rc := AddReceipt{ID: u.id, Warnings: []string{}}
		if err := addUnit(opt.from, opt.dir, u, &rc); err != nil {
			return err
		}
		receipts = append(receipts, rc)
	}
	proveErr := prove(opt.dir, opt.units, io.Discard)
	if opt.format == planFormatJSON {
		env := addEnvelope{
			Unit: opt.unitID, From: opt.from, Dir: opt.dir,
			Receipts: receipts, BuildOk: proveErr == nil,
		}
		if err := printAddJSON(stdout, env); err != nil {
			return err
		}
		return proveErr
	}
	for i := range receipts {
		printAddReceipt(stdout, &receipts[i])
	}
	if proveErr != nil {
		return proveErr
	}
	fmt.Fprintln(stdout, "gogogo: done — review the diff, then run `make dev`")
	return nil
}

// parseAddArgs parses flags, resolves the unit, and validates paths.
func parseAddArgs(args []string, stdout *os.File) (addOptions, error) {
	var opt addOptions
	fs := flag.NewFlagSet("gogogo add", flag.ContinueOnError)
	fs.StringVar(&opt.from, "from", "", "pristine template checkout to copy from (required)")
	fs.StringVar(&opt.dir, "dir", "", "target project checkout (required)")
	fs.BoolVar(&opt.yes, "yes", false, "apply without asking (agents: always pin this)")
	fs.BoolVar(&opt.dryRun, "dry-run", false, "print the plan and stop; changes nothing")
	fs.StringVar(&opt.format, "format", planFormatText, "plan format: text|json")
	fs.SetOutput(stdout)
	// Accept the unit before flags (`add whiteboard --from …`): the
	// stdlib flag parser stops at the first positional, so pull it out
	// first to support both orders.
	cliArgs := args
	var positional []string
	if len(cliArgs) > 0 && !strings.HasPrefix(cliArgs[0], "-") {
		positional = []string{cliArgs[0]}
		cliArgs = cliArgs[1:]
	}
	if err := fs.Parse(cliArgs); err != nil {
		return opt, err
	}
	if opt.format != planFormatText && opt.format != planFormatJSON {
		return opt, fmt.Errorf("unknown --format %q (want text|json)", opt.format)
	}
	names := make([]string, 0, len(positional)+len(fs.Args()))
	names = append(names, positional...)
	names = append(names, fs.Args()...)
	if len(names) != 1 {
		fs.Usage()
		return opt, fmt.Errorf("usage: gogogo add <unit> --from TEMPLATE --dir PROJECT")
	}
	opt.unitID = names[0]
	units, err := addClosure(names[0])
	if err != nil {
		return opt, err
	}
	opt.units = units
	if opt.from == "" || opt.dir == "" {
		fs.Usage()
		return opt, fmt.Errorf("--from and --dir are both required")
	}
	if _, err := os.Stat(opt.from); err != nil {
		return opt, &exitError{code: 1, msg: "template source not found: " + opt.from}
	}
	return opt, nil
}

func confirmAdd(stdout, stdin *os.File, unitID string) bool {
	fmt.Fprintf(stdout, "Add unit %q from template? [y/N]: ", unitID)
	var answer [8]byte
	n, _ := stdin.Read(answer[:])
	resp := strings.ToLower(strings.TrimSpace(string(answer[:n])))
	return resp == "y" || resp == "yes"
}

// printAddPlan previews an add: unit metadata plus what would change.
func printAddPlan(w io.Writer, format string, u trimUnit, from, dir string) {
	if format == planFormatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"unit": u.id, "from": from, "dir": dir,
			"summary": unitOneLiner(u), "dryRun": true,
		})
		return
	}
	m := u.meta()
	fmt.Fprintf(w, "gogogo: add %s (%s) from %s into %s\n", u.id, m.kind, from, dir)
	fmt.Fprintf(w, "  copies %d path(s), %d wiring strip(s) to restore\n",
		len(m.dirs)+len(m.files),
		len(u.mainStrips)+len(u.desktopStrips)+len(u.extraStrips))
	for _, warn := range addFollowUps(u) {
		fmt.Fprintf(w, "  manual follow-up: %s\n", warn)
	}
}

func printAddJSON(w io.Writer, env addEnvelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// printAddReceipt reports counts plus follow-ups.
func printAddReceipt(w io.Writer, rc *AddReceipt) {
	fmt.Fprintf(w, "gogogo: receipt: %d dir(s), %d file(s) copied, %d line(s) inserted\n",
		rc.DirsCopied, rc.FilesCopied, rc.LinesInserted)
	for _, warn := range rc.Warnings {
		fmt.Fprintf(w, "gogogo: follow-up: %s\n", warn)
	}
}
