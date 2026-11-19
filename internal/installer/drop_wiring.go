// Installer: applying a dropped unit's declared wiring edits.
//
// A builtinUnit already carries, per file, the exact lines that wire it
// (extraDrops) and the functions that exist only to serve it (extraStrips).
// Until this file existed those fields were read ONLY to describe a port bundle
// (export_wiring.go): nothing applied them to a scaffolded tree. So dropping a
// feature removed its own directory and left every shared line behind — which
// is how internal/collab survived losing whiteboard and notes, keeping loro-go
// (a CGO binding) in go.mod and a C toolchain on every build.
//
// The design rule here is that the DECLARATION is the source of truth: this
// file holds no knowledge of which units exist or what they wire. It reads the
// unit registry and applies what the unit says, so a new unit gets working
// removal by declaring its drops accurately, and a stale declaration is a
// visible bug in the unit rather than a silent survivor in the tree.
package installer

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// applyDropEdits applies one unit's declared wiring edits to a tree. Lines are
// removed whole, which is exactly what fileDrop documents, and a file that does
// not exist is success (another unit may have removed it already).
//
// It runs BEFORE the proof build, never after: if a declaration is stale the
// build fails and the operator sees it, instead of shipping a tree whose server
// still imports a package the plan says is gone.
func applyDropEdits(tree *treeFS, u builtinUnit) error {
	if err := applyLineDrops(tree, u); err != nil {
		return err
	}
	if err := applyBlockStrips(tree, u); err != nil {
		return err
	}
	return applyDesktopDrops(tree, u)
}

// applyLineDrops removes the declared whole lines: the extra wiring, and the
// test lines that construct what the unit removes.
func applyLineDrops(tree *treeFS, u builtinUnit) error {
	for _, list := range [][]fileDrop{u.extraDrops, u.testDrops} {
		for _, d := range list {
			if err := dropLinesContaining(tree, d.path, d.substrs); err != nil {
				return fmt.Errorf("%s: %w", u.id, err)
			}
		}
	}
	return nil
}

// applyBlockStrips removes the declared brace blocks, head and body together.
// Splitting them from the line drops is what keeps a branch from leaving its
// condition behind.
func applyBlockStrips(tree *treeFS, u builtinUnit) error {
	if err := applyStrips(tree, u.id, chiGoFile, u.blockStrips); err != nil {
		return err
	}
	// mainStrips target cmd/web/main.go, the program's own entrypoint. They were
	// declared and read by nothing: the export bundle walked extraStrips only, so
	// a trim left `stack.AttachPresenceFederation` calling a method the same run
	// had deleted, and the operator's first build failed on the program's main.
	if err := applyStrips(tree, u.id, desktopWebMainFile, u.mainStrips); err != nil {
		return err
	}
	for _, s := range u.extraStrips {
		if err := applyStrips(tree, u.id, s.path, s.rules); err != nil {
			return err
		}
	}
	return nil
}

// applyDesktopDrops handles cmd/desktop: a SEPARATE target that still sits in the
// same module, so `go mod tidy` parses it. An import left behind there keeps the
// package (and its CGO dependency) in the module graph even though the web binary
// no longer names it — how loro-go survived a tree with no collaborative feature
// left.
func applyDesktopDrops(tree *treeFS, u builtinUnit) error {
	if len(u.desktopDropLines) > 0 {
		if err := dropLinesContaining(tree, desktopMainFile, u.desktopDropLines); err != nil {
			return fmt.Errorf("%s: %w", u.id, err)
		}
	}
	if err := applyStrips(tree, u.id, desktopMainFile, u.desktopStrips); err != nil {
		return err
	}
	if len(u.goModDrops) > 0 {
		if err := dropRequirements(tree, u.goModDrops); err != nil {
			return fmt.Errorf("%s: %w", u.id, err)
		}
	}
	return nil
}

// dropRequirements removes the `require` lines for the given module paths from
// go.mod, along with the `replace` pair a fork lives behind.
//
// It is a text edit on purpose: `go mod tidy` in the proof step is the authority
// on what the module graph needs, and it runs right after. Removing the line
// here is what lets tidy actually drop it — tidy alone never deletes a
// requirement no package imports, so a CGO binding would stay pinned and keep
// demanding a C toolchain on every platform.
func dropRequirements(tree *treeFS, modules []string) error {
	path := filepath.Join(tree.root, "go.mod")
	raw, err := tree.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(raw), "\n")
	out := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		if slices.ContainsFunc(modules, func(m string) bool {
			// A require/replace line names the module; match on the module path
			// so `=> fork v0.5.1` and `v0.5.0` both go.
			return strings.Contains(line, m)
		}) && (strings.HasPrefix(strings.TrimSpace(line), "require") ||
			strings.HasPrefix(strings.TrimSpace(line), "replace") ||
			strings.Contains(line, " // indirect")) {
			changed = true
			continue
		}
		out = append(out, line)
	}
	if !changed {
		return nil
	}
	return tree.WriteFile(path, []byte(strings.Join(out, "\n")), sourceFileMode)
}

// dropLinesContaining removes every line of one file containing any of substrs.
//
// A removed line that OPENS a brace block (`if perr != nil {`) takes its block
// with it, closing line included. Dropping only the text-matched lines left the
// `if` head behind in the template's chi.go, which then failed to parse as
// "undefined: perr" — a build error blamed on the operator's first edit rather
// than on this declaration. Counting braces on the removed line is what makes a
// line-oriented delete safe on block-structured code.
//
// The file is only rewritten when something matched, so a unit whose wiring has
// already moved on leaves the tree untouched instead of churning mtime/format.
func dropLinesContaining(tree *treeFS, relPath string, substrs []string) error {
	if len(substrs) == 0 {
		return nil
	}
	path := filepath.Join(tree.root, filepath.FromSlash(relPath))
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(raw), "\n")
	kept := make([]string, 0, len(lines))
	changed := false
	for i := 0; i < len(lines); i++ {
		if !slices.ContainsFunc(substrs, func(s string) bool { return strings.Contains(lines[i], s) }) {
			kept = append(kept, lines[i])
			continue
		}
		changed = true
		// Balance the braces the removed line carried: a block that opened on a
		// line we just deleted must not leave its body and `}` behind.
		depth := countBraceDelta(lines[i])
		for depth > 0 && i+1 < len(lines) {
			i++
			depth += countBraceDelta(lines[i])
		}
	}
	if !changed {
		return nil
	}
	return writeSourceLines(tree, path, kept)
}

// countBraceDelta is the net `{` minus `}` on one line, ignoring the ones that
// appear inside a string or a comment badly enough to break a delete. Lines the
// installer drops are wiring statements, where braces are structural.
func countBraceDelta(line string) int {
	return strings.Count(line, "{") - strings.Count(line, "}")
}

// stripMarkedBlock removes the span a stripRule marks.
//
// Two forms are supported, because the units use both:
//
//   - startMarker .. endMarker: the span ends on the line carrying endMarker
//     (inclusive) — the helper function and its comment above it.
//   - startMarker + endIsClosingBrace: the span ends on the `}` that closes the
//     block the marker opened, found by brace counting. The port bundle needs an
//     explicit `endBrace` for a faithful copy; a scaffolded tree does not, since
//     the build judges the result either way.
//
// An unanchored rule whose marker is absent is a no-op, never an error: a unit
// that declares a block the tree no longer has is describing stale sourcing, and
// refusing the whole scaffold over it would punish the operator for it.
func stripMarkedBlock(tree *treeFS, relPath string, r stripRule) error {
	if r.startMarker == "" || (r.endMarker == "" && !r.endIsClosingBrace) {
		return nil
	}
	path := filepath.Join(tree.root, filepath.FromSlash(relPath))
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	lines := strings.Split(string(raw), "\n")
	start := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, r.startMarker) })
	if start < 0 {
		return nil
	}
	end := blockEndLine(lines, start, r)
	if end < 0 {
		return nil
	}
	out := make([]string, 0, len(lines)-(end-start))
	out = append(out, lines[:start]...)
	out = append(out, lines[end+1:]...)
	return writeSourceLines(tree, path, out)
}

// blockEndLine is the last line of the span a stripRule selects, or -1.
func blockEndLine(lines []string, start int, r stripRule) int {
	if r.endMarker != "" {
		for i := start + 1; i < len(lines); i++ {
			if strings.Contains(lines[i], r.endMarker) {
				return i
			}
		}
		return -1
	}
	// endIsClosingBrace: walk until the block the marker opened is balanced.
	//
	// A marker may be DOC immediately above the construct it describes — the
	// bootCollab case, where the comment names the helper below it. Starting the
	// depth count on the comment makes delta 0 and stops on the very next line,
	// leaving the function behind. So the walk starts at the first line that
	// actually opens a brace.
	open := start
	for open < len(lines) && countBraceDelta(lines[open]) <= 0 {
		open++
	}
	if open >= len(lines) {
		return -1
	}
	depth := countBraceDelta(lines[open])
	for i := open + 1; i < len(lines); i++ {
		depth += countBraceDelta(lines[i])
		if depth <= 0 {
			return i
		}
	}
	return -1
}

// writeSourceLines writes back a Go file that had lines removed. gofmt is not
// run here: the scaffold's proof step already builds the tree, and a formatting
// pass over a half-declared edit would hide a syntax error behind a reformat.
func writeSourceLines(tree *treeFS, path string, lines []string) error {
	body := strings.Join(lines, "\n")
	// Collapse the blank-line run a deletion can leave behind, so the result
	// still reads like code someone wrote.
	for strings.Contains(body, "\n\n\n") {
		body = strings.ReplaceAll(body, "\n\n\n", "\n\n")
	}
	if err := tree.WriteFile(path, []byte(body), sourceFileMode); err != nil {
		return err
	}
	return nil
}

// removeUnitCapabilities removes the directories and owned files of every
// capability a dropped unit carries, EXCEPT the one being uninstalled (its
// receipt already handled it).
//
// The pairing comes from unitCaps, which is the installer's own statement of
// what ships together — one list, not a second guess. A capability with no
// directory and no files (genkit, a module dependency) is a no-op here: dropping
// its unit plus `go mod tidy` is what removes it from the build.
func removeUnitCapabilities(tree *treeFS, u builtinUnit, drop []string) error {
	byID := capabilities.ByID()
	for _, capID := range u.caps() {
		if capID == u.id {
			continue
		}
		c, ok := byID[capID]
		if !ok {
			continue
		}
		// A capability the registry marks as another unit's SLICE may be kept by
		// the operator: shape api drops notes and whiteboard, which orphans
		// collab, while a run that drops only whiteboard must keep the transport
		// notes still imports. Keying on the drop list keeps one rule — a
		// capability goes when every slice that carries it went — instead of a
		// bundle list that has to match unitCaps by hand.
		if keptByAnotherSlice(capID, drop) {
			continue
		}
		for _, dir := range c.Dirs {
			if err := tree.RemoveAll(filepath.Join(tree.root, filepath.FromSlash(dir))); err != nil {
				return fmt.Errorf("removing %s: %w", dir, err)
			}
		}
		// A file this capability OWNS may be shared with another one, so it only
		// goes when the package it IMPORTS is gone. presence.go is the case: it is
		// whiteboard's declared file, but notes is often the last consumer, so
		// keying on the recipe that happened to run never matched. The import list
		// is already the declaration and the filesystem is already the answer.
		for _, f := range c.Files {
			path := filepath.Join(tree.root, filepath.FromSlash(f))
			orphaned, serr := fileImportsMissingPackage(tree, path)
			if serr != nil {
				return fmt.Errorf("inspecting %s: %w", f, serr)
			}
			if !orphaned {
				continue
			}
			if err := tree.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("removing %s: %w", f, err)
			}
		}
	}
	return nil
}

// keptByAnotherSlice reports whether a capability is a SLICE that survives this
// scaffold, in which case removing its package would break the tree.
//
// Slices are the capabilities with a recipe receipt; a servant like collab has
// none, so it is never kept on its own behalf — only as a dependency of a slice
// that stays.
func keptByAnotherSlice(capID string, drop []string) bool {
	if !slices.Contains(demoSlices, capID) {
		return false
	}
	return !slices.Contains(drop, capID)
}

// builtinUnitByID looks a unit up in the canonical list.
func builtinUnitByID(id string) (builtinUnit, bool) {
	for _, u := range builtinUnits {
		if u.id == id {
			return u, true
		}
	}
	return builtinUnit{}, false
}

// dropReceiptServants removes the servant directories that belong to a recipe,
// when the recipe itself is what kept them.
//
// Ownership is declared once, in `servantOwners`, and read here rather than
// re-derived. An importer scan of the whole tree was attempted instead and
// reverted: feature slices mount through units.gen.go, which is regenerated only
// after this point, so at sweep time nothing imported the product's own features
// and a run deleted genui, license and room. Declared ownership cannot confuse a
// slice with a servant.
func dropReceiptServants(tree *treeFS, recipeID string) error {
	// No early return on a missing entry: the FILE sweep below must run for every
	// uninstall. notes has no servantOwners entry, yet it is usually the last
	// consumer of the transport, and returning early here left presence.go
	// importing a package the same run had deleted — a broken tree, reported as
	// the operator's own build error.
	for _, dir := range servantOwners[recipeID] {
		if err := tree.RemoveAll(filepath.Join(tree.root, filepath.FromSlash(dir))); err != nil {
			return fmt.Errorf("removing %s: %w", dir, err)
		}
	}
	return removeOrphanedOwnedFiles(tree)
}

// removeOrphanedOwnedFiles removes every capability-owned Go file whose import
// names a package that no longer exists.
//
// It is separate from dropReceiptServants because BOTH removal paths need it and
// only one had it: the uninstall swept orphaned files, and a scaffold dropping the
// same slices left presence.go importing the directory it had just deleted, so a
// clean `--recipes none` still failed `go mod tidy`.
//
// presence.go is the file that motivated it: owned by whiteboard, usually last
// used by notes, so keying removal on whichever recipe happened to run never
// matched. Its import is internal/collab, and that is what decides. Non-Go owned
// files are skipped — c.Files lists assets too, and parsing one as Go aborted the
// whole removal with a JavaScript syntax error.
func removeOrphanedOwnedFiles(tree *treeFS) error {
	for _, c := range capabilities.All {
		for _, f := range c.Files {
			if !strings.HasSuffix(f, ".go") {
				continue
			}
			path := filepath.Join(tree.root, filepath.FromSlash(f))
			orphaned, err := fileImportsMissingPackage(tree, path)
			if err != nil {
				return fmt.Errorf("inspecting %s: %w", f, err)
			}
			if !orphaned {
				continue
			}
			if rerr := tree.Remove(path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				return fmt.Errorf("removing %s: %w", f, rerr)
			}
		}
	}
	return nil
}

// fileImportsMissingPackage reports whether a file imports an intra-module
// package whose directory no longer exists.
//
// It is the precise form of "did the thing this file wires just leave?" and needs
// no pairing declaration: the import list is already the declaration, and the
// filesystem is already the answer. A file with no intra-module import is never
// orphaned by a removal, so it stays.
func fileImportsMissingPackage(tree *treeFS, path string) (bool, error) {
	raw, err := tree.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	f, perr := parser.ParseFile(token.NewFileSet(), path, raw, parser.ImportsOnly)
	if perr != nil {
		// Reported, not swallowed: a file we cannot read may be exactly the one
		// importing a package this run deleted, and guessing "not orphaned" would
		// keep a broken file in the tree.
		return false, fmt.Errorf("parsing %s: %w", path, perr)
	}
	modPath, merr := modulePath(tree.root)
	if merr != nil {
		return false, fmt.Errorf("reading the module path: %w", merr)
	}
	if modPath == "" {
		return false, nil // no module line: nothing can be orphaned by an import
	}
	for _, spec := range f.Imports {
		p, uerr := strconv.Unquote(spec.Path.Value)
		if uerr != nil || !strings.HasPrefix(p, modPath+"/") {
			continue
		}
		dir := filepath.Join(tree.root, filepath.FromSlash(strings.TrimPrefix(p, modPath+"/")))
		if _, statErr := os.Stat(dir); statErr == nil {
			return false, nil
		}
		return true, nil
	}
	return false, nil
}

// applyStrips applies each rule to path, using the rule's own path when it names
// one. Split out so the three strip lists share one loop instead of growing a
// fourth copy of it.
func applyStrips(tree *treeFS, unitID, path string, rules []stripRule) error {
	for _, r := range rules {
		target := path
		if r.path != "" {
			target = r.path
		}
		if err := stripMarkedBlock(tree, target, r); err != nil {
			return fmt.Errorf("%s: %w", unitID, err)
		}
	}
	return nil
}
