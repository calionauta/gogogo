// Installer: the declarations must describe the tree they will edit.
//
// drop_wiring.go states the design rule plainly — "a stale declaration is a
// visible bug in the unit rather than a silent survivor in the tree". This file
// is what makes it visible. Without it the rule was aspiration: applying a drop
// is a no-op when the marker is missing (dropLinesContaining treats a missing
// line as already-done, and applyBlockStrips skips a marker it cannot find), so
// a refactor that MOVED a wiring line left the declaration pointing at the old
// file and every trim silently stopped removing it.
//
// That is not hypothetical. When these declarations were first audited against
// the real repo, the whiteboard unit still named
//
//	MountSubtree(r, whiteboard.AssetPath, whiteboard.StaticHandler())
//
// in internal/server/chi.go, but the mount had moved into
// features/whiteboard/handlers/register.go and the chi.go line no longer
// existed anywhere. Trimming whiteboard would have left the feature wired with
// its own directory deleted — the exact class of bug drop_wiring.go was written
// to end, one layer down.
//
// The check is deliberately cheap and textual: it reads the real repo, not a
// fixture, and asserts only that each declared marker still occurs in the file
// the declaration names. It does NOT assert the trim produces a building tree —
// scaffold-chain does that end to end. This one catches the drift at the
// earliest point, before anyone runs a trim.

package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// staleDeclarations lists markers that are knowingly not present. Every entry
// needs a reason, and the list is expected to shrink, never grow silently: a
// new entry means a declaration is describing a tree that no longer exists.
//
// Keep this empty if you can. An entry here is a trim that will not remove what
// it claims to remove.
var staleDeclarations = map[string]string{}

// readRepoFile reads a slash-separated repo path, reporting absence.
func readRepoFile(root, rel string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// stripDecl is one declaration and the file it defaults to, so the failure
// message can name both.
type stripDecl struct {
	unit string
	path string
	rule stripRule
}

// collectStripDecls flattens every declared block strip, including the ones
// that name their own file, into a single list.
func collectStripDecls() []stripDecl {
	var out []stripDecl
	add := func(u builtinUnit, defaultPath string, rules []stripRule) {
		for _, r := range rules {
			p := defaultPath
			if r.path != "" {
				p = r.path
			}
			out = append(out, stripDecl{unit: u.id, path: p, rule: r})
		}
	}
	for _, u := range builtinUnits {
		add(u, chiGoFile, u.blockStrips)
		add(u, desktopWebMainFile, u.mainStrips)
		add(u, desktopMainFile, u.desktopStrips)
		for _, fs := range u.extraStrips {
			add(u, fs.path, fs.rules)
		}
	}
	return out
}

// TestDeclaredStripMarkersExist proves every block marker a unit declares is
// still in the file that unit names.
func TestDeclaredStripMarkersExist(t *testing.T) {
	root := repoRoot(t)
	cache := map[string]string{}
	read := func(rel string) (string, bool) {
		if body, ok := cache[rel]; ok {
			return body, true
		}
		body, ok := readRepoFile(root, rel)
		if ok {
			cache[rel] = body
		}
		return body, ok
	}

	for _, d := range collectStripDecls() {
		body, ok := read(d.path)
		if !ok {
			t.Errorf("unit %s declares a strip in %s, which does not exist", d.unit, d.path)
			continue
		}
		if d.rule.startMarker != "" && !strings.Contains(body, d.rule.startMarker) {
			t.Errorf("unit %s: strip start marker missing from %s:\n  want substring: %q",
				d.unit, d.path, d.rule.startMarker)
		}
		if d.rule.endMarker != "" && !strings.Contains(body, d.rule.endMarker) {
			t.Errorf("unit %s: strip end marker missing from %s:\n  want substring: %q",
				d.unit, d.path, d.rule.endMarker)
		}
	}
}

// TestDeclaredDropLinesExist proves every whole-line drop a unit declares still
// matches a line, and that every module it declares for removal is in go.mod.
//
// A line drop that matches nothing is the quieter failure of the two: the trim
// reports success, the wiring stays, and nothing fails until someone builds the
// dropped tree and finds a call to a deleted package.
func TestDeclaredDropLinesExist(t *testing.T) {
	root := repoRoot(t)
	gomod, ok := readRepoFile(root, "go.mod")
	if !ok {
		t.Fatal("go.mod not found")
	}

	for _, u := range builtinUnits {
		for _, list := range [][]fileDrop{u.extraDrops, u.testDrops} {
			for _, d := range list {
				body, ok := readRepoFile(root, d.path)
				if !ok {
					t.Errorf("unit %s drops lines from %s, which does not exist", u.id, d.path)
					continue
				}
				for _, sub := range d.substrs {
					if !strings.Contains(body, sub) {
						t.Errorf("unit %s: drop line missing from %s:\n  want substring: %q",
							u.id, d.path, sub)
					}
				}
			}
		}
		for _, sub := range u.desktopDropLines {
			body, ok := readRepoFile(root, desktopMainFile)
			if !ok {
				t.Fatalf("unit %s drops from %s, which does not exist", u.id, desktopMainFile)
			}
			if !strings.Contains(body, sub) {
				t.Errorf("unit %s: desktop drop line missing from %s:\n  want substring: %q",
					u.id, desktopMainFile, sub)
			}
		}
		for _, mod := range u.goModDrops {
			if strings.Contains(gomod, mod) {
				continue
			}
			key := u.id + " goModDrops " + mod
			if reason, known := staleDeclarations[key]; known {
				t.Logf("unit %s: known drift — %s (%s)", u.id, mod, reason)
				continue
			}
			t.Errorf("unit %s declares goModDrops for %q, which is not in go.mod.\n"+
				"  A module that is not there cannot be dropped: fix the declaration, or\n"+
				"  add %q to staleDeclarations with a reason.", u.id, mod, key)
		}
	}
}
