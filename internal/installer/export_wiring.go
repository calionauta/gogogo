// Installer engine: the surface `export` renders.
//
// These were the add command's mechanics of listing what it would restore.
// With `gogogo add` retired (phase 4: one-way removal is preferable to
// anchor-patching that cannot see a moved core), they survive as the port
// tool's lists: for a foreign tree, the hand-restored lines are exactly the
// points below the tool refuses to invent. The span insertion machinery
// around them is deleted; listing stays.
package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// Insertion anchors for the port surface. main.go after the shutdown defer;
// the desktop's Wails boot section. A rule's own addBefore/addAfter wins over
// the path default. TestEveryDeclaredAddAnchorIsLive died with `add`; the
// --check gate verifies these defaults against the tree until its own
// retirement.
const (
	mainCallsAnchor    = "\tdefer stopLifecycle()"
	desktopBlockAnchor = "\tviewURL := webviewURL(cfg.Host, port)"
)

// Shared file paths for the port and scaffold mechanics (single spelling).
const (
	dirMode = 0o755
	// gitDirName is the checkout bookkeeping directory: never part of a
	// copied recipe, and never a rename target.
	gitDirName = ".git"
)

// spanAnchor locates an insertion point: before/after the first line exactly
// equal to the marker.
type spanAnchor struct {
	before string
	after  string
}

func anchorBefore(line string) spanAnchor { return spanAnchor{before: line} }
func anchorAfter(line string) spanAnchor  { return spanAnchor{after: line} }

func addSpanAnchor(path string) spanAnchor {
	switch path {
	case filepath.Join("cmd", "web", "main.go"):
		return anchorAfter(mainCallsAnchor)
	case filepath.Join("cmd", "desktop", "main.go"):
		return anchorBefore(desktopBlockAnchor)
	default:
		return spanAnchor{}
	}
}

// goModModule reads the module path from a go.mod file. `gogogo new` needs it
// to write imports in the target module's spelling.
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

// routerCallLines are the exact Init call lines a unit owns: explicit
// routerCalls plus router.go line-drops that are calls, not imports.
// Import-lookalikes (ending in a quote) are restored elsewhere and are not
// listed as wiring.
func routerCallLines(u builtinUnit) []string {
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
		if ed.path != chiGoFile {
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

// isRestorableRouteCall reports whether a chi.go drop line is a route
// registration at the composition root, which the port lists as wiring that
// lands mechanically rather than as a manual hand-restore.
func isRestorableRouteCall(sub string) bool {
	return strings.HasPrefix(sub, "mux.Handle") ||
		strings.Contains(sub, ".MountFeature(") ||
		strings.Contains(sub, ".MountSubtree(") ||
		strings.Contains(sub, ".MountPath(") ||
		strings.HasPrefix(sub, "r.NotFound(") ||
		strings.HasPrefix(sub, "r.Get(")
}

// portManualWiring lists the trim edits no tool can reverse mechanically:
// chi.go lines that are not route registrations, unanchored strip blocks,
// one-way replaces, and desktop imports. In a foreign tree these are the
// hand-restored lines, printed verbatim so they can be pasted, not summarized.
func portManualWiring(u builtinUnit) []string {
	var out []string
	for _, ed := range u.extraDrops {
		for _, sub := range ed.substrs {
			if ed.path == chiGoFile && isRestorableRouteCall(strings.TrimSpace(sub)) {
				continue
			}
			if strings.HasSuffix(sub, `"`) {
				continue
			}
			out = append(out, ed.path+": "+sub)
		}
	}
	for _, es := range u.extraStrips {
		restorable := addSpanAnchor(es.path) != (spanAnchor{})
		if !restorable {
			for _, r := range es.rules {
				if r.addBefore != "" || r.addAfter != "" {
					restorable = true
					break
				}
			}
		}
		if !restorable {
			for _, r := range es.rules {
				out = append(out, es.path+": block "+r.startMarker)
			}
		}
	}
	for _, rp := range u.replaces {
		out = append(out, rp.path+": one-way replace "+rp.old)
	}
	for _, line := range u.desktopDropLines {
		out = append(out, "cmd/desktop/main.go: import "+line)
	}
	return out
}

// portFollowUps lists the manual steps no tool can do: .templ call sites
// (positions vary per layout) and the navbar brand (user taste).
func portFollowUps(u builtinUnit) []string {
	var out []string
	switch u.id {
	case unitWhiteboard:
	case unitSounds:
		out = append(out,
			"sounds has no UI until call sites return: re-add "+
				"@sounds.SoundAssets() to each page's head wrapper and "+
				"@sounds.SoundSwitch() to its *Sound() wrapper, then `make templ` "+
				"(checklist in features/sounds/sounds.go).")
	case unitConfigView:
		out = append(out,
			"TestBuildChiMux still pins /config as 404: re-assert the 403 "+
				"admin fence in internal/server/chi_test.go now the route is back.")
	case unitGoAkt:
		out = append(out,
			"TestBuildChiMux pins fewer demo routes: re-add \"/room\" to "+
				"its target table in internal/server/chi_test.go if the engine is back.")
	case unitCredits:
		out = append(out,
			"TestBuildChiMux still pins /api/credits as 404: re-assert "+
				"401 JSON in internal/server/chi_test.go now the route is back.")
	case unitGenUI:
		out = append(out,
			"TestBuildChiMux pins fewer demo routes: re-add \"/chat\" "+
				"to its target table in internal/server/chi_test.go "+
				"if the bundle is the front door again.")
	}
	return out
}

// portClosure orders unit + its transitive deps (deps first), so the port
// bundle carries a unit's soft dependencies (whiteboard needs sounds' package).
func portClosure(unitID string) ([]builtinUnit, error) {
	byUnit := map[string]builtinUnit{}
	for _, u := range builtinUnits {
		byUnit[u.id] = u
	}
	byCap := capabilities.ByID()
	unitOf := map[string]string{}
	for _, u := range builtinUnits {
		for _, c := range u.caps() {
			unitOf[c] = u.id
		}
	}
	var ordered []builtinUnit
	visited := map[string]bool{}
	var visit func(id string) error
	visit = func(id string) error {
		if visited[id] {
			return nil
		}
		visited[id] = true
		u, ok := byUnit[id]
		if !ok {
			return fmt.Errorf("unknown unit %q (valid: %s)", id, strings.Join(dropIDs(builtinUnits), ", "))
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

// sourceRelease reports the release a template checkout is at, read from the
// top of its changelog. Empty means "not knowable" — an extracted tarball, a
// shallow export, a dev build — and the export bundle records it as-is.
func sourceRelease(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "CHANGELOG.md"))
	if err != nil {
		return ""
	}
	releases := parseChangelog(string(raw))
	if len(releases) == 0 {
		return ""
	}
	return strings.TrimPrefix(releases[0].Version, "v")
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

// looksLikeTemplate probes whether root is a gogogo checkout. Used by the
// port and recipe paths to refuse a foreign tree with one actionable line
// instead of per-unit noise.
func looksLikeTemplate(root string) bool {
	for _, probe := range []string{
		filepath.Join("cmd", "web", "main.go"),
		filepath.Join("internal", "installer", "run.go"),
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(probe))); err == nil {
			return true
		}
	}
	return false
}
