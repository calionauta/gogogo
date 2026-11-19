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
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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
	for _, d := range u.extraDrops {
		if err := dropLinesContaining(tree, d.path, d.substrs); err != nil {
			return fmt.Errorf("%s: %w", u.id, err)
		}
	}
	for _, s := range u.extraStrips {
		for _, r := range s.rules {
			if err := stripMarkedBlock(tree, s.path, r); err != nil {
				return fmt.Errorf("%s: %w", u.id, err)
			}
		}
	}
	// cmd/desktop is a SEPARATE target that still sits in the same module, so
	// `go mod tidy` parses it: an import left behind there keeps the package (and
	// its CGO dependency) in the module graph even though the web binary no
	// longer names it. That is how loro-go survived a tree with no collaborative
	// feature left.
	if len(u.desktopDropLines) > 0 {
		if err := dropLinesContaining(tree, desktopMainFile, u.desktopDropLines); err != nil {
			return fmt.Errorf("%s: %w", u.id, err)
		}
	}
	for _, r := range u.desktopStrips {
		if err := stripMarkedBlock(tree, desktopMainFile, r); err != nil {
			return fmt.Errorf("%s: %w", u.id, err)
		}
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
	depth := countBraceDelta(lines[start])
	for i := start + 1; i < len(lines); i++ {
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
func removeUnitCapabilities(tree *treeFS, u builtinUnit) error {
	byID := capabilities.ByID()
	for _, capID := range u.caps() {
		if capID == u.id {
			continue
		}
		c, ok := byID[capID]
		if !ok {
			continue
		}
		for _, dir := range c.Dirs {
			if err := tree.RemoveAll(filepath.Join(tree.root, filepath.FromSlash(dir))); err != nil {
				return fmt.Errorf("removing %s: %w", dir, err)
			}
		}
		for _, f := range c.Files {
			path := filepath.Join(tree.root, filepath.FromSlash(f))
			if err := tree.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("removing %s: %w", f, err)
			}
		}
	}
	return nil
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
