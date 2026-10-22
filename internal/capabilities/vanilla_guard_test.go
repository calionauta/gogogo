// SCOPE:core — vanilla-JS allowlist guard (locality-of-behavior enforcement).
//
// Datastar owns behavior; vanilla JS is the last resort (see the skill's
// references/datastar.md item 3: enumerate the primitive first, and when
// none fits, write the reason next to the script). datastar-lint checks
// the Datastar surface, not the presence of vanilla — so nothing blocked
// a new addEventListener until this test. It scans every *.templ under
// features/, web/ and internal/ for the three markers that must route
// through Datastar (addEventListener, setInterval, .innerHTML) and fails
// on anything outside the allowlist below.
//
// Adding a NEW vanilla island: extend vanillaAllow with the file, the
// exact line needle, and the Datastar primitive you ruled out and why.
// The stale-entry check fails on allowlist rows that no longer match,
// so converting an island means deleting its row — the list only shrinks.
package capabilities

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vanillaMarker is a JS shape that must route through Datastar.
var vanillaMarkers = []string{"addEventListener(", "setInterval(", ".innerHTML"}

// vanillaAllowed is one justified exception: needle must match the exact
// source line(s), reason names the primitive considered and rejected.
type vanillaAllowed struct {
	needle string
	reason string
}

// vanillaAllow pins every legitimate vanilla island. Each reason was
// proven against the primitive list — a new entry needs the same proof.
var vanillaAllow = map[string][]vanillaAllowed{
	// SW postMessage bridge: no Datastar primitive receives service-worker
	// messages. The bridge already re-enters Datastar by dispatching
	// gogogo:queued (see data-on:gogogo:queued__window consumers); the
	// online/offline pair already moved to data-on attributes on the div.
	"internal/components/offline_banner.templ": {
		{`navigator.serviceWorker.addEventListener("message", function (e) {`, "SW postMessage has no Datastar primitive"},
	},
	// Nav link row: ONE <details> serves the inline row and the collapsed
	// menu, so its `open` state must follow the stylesheet's breakpoint.
	// Ruled out: data-attr:open driven by a $navOpen signal — the breakpoint
	// is a media query, which no signal can express at init, and a native
	// summary click already toggles the attribute, so the signal would have
	// to fight it (data-on:click__prevent then reimplementing keyboard and
	// no-JS behavior). Ruled out moving it back to theme.js: that runs deferred,
	// so a tablet paints the open dropdown over the page first (this script
	// runs at parse time, before paint). Never morphed: the navbar sits
	// outside every patched region.
	"features/authviews/navbar.templ": {
		{
			needle: `if (wide.addEventListener) wide.addEventListener('change', sync);`,
			reason: "media-query breakpoint cannot be a Datastar signal; " +
				"driving data-attr:open would also fight the native <details> toggle",
		},
	},
	// Basecoat runtime bootstrap. The skin ships a third-party component
	// library whose entry point is `basecoat.initAll()`; it must run once the
	// DOM exists, after the deferred vendor bundle has defined `basecoat`.
	// Ruled out data-on:load / data-effect: Datastar owns those, they re-run on
	// every morphed patch, and `initAll` is an idempotent-once init rather than
	// a reactive expression — re-running it per patch is exactly the kind of
	// "works until the list re-renders" bug this guard exists to prevent.
	// The block moved here from a templ.Raw string in skin.go, which put it
	// outside this guard's reach; moving it into the .templ tree is what made
	// it visible, so it is recorded rather than re-hidden.
	"web/skins/basecoat/assets.templ": {
		{
			needle: `document.addEventListener('DOMContentLoaded', function() {`,
			reason: "vendor runtime needs one initAll() after DOM ready, after the " +
				"deferred basecoat bundle; Datastar's data-on/data-effect re-run per " +
				"patch, which double-initialises the vendor components",
		},
	},
}

func TestNoNewVanillaJS(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	used := map[string]map[string]bool{}
	for rel := range vanillaAllow {
		used[rel] = map[string]bool{}
	}
	var violations []string
	var templPaths []string
	for _, dir := range []string{"features", "web", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".templ") {
				return err
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			templPaths = append(templPaths, rel)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	// Read outside the walk callback: gosec G122 forbids filesystem
	// operations on walked paths inside it (symlink TOCTOU).
	for _, rel := range templPaths {
		raw, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		for line := range strings.SplitSeq(string(raw), "\n") {
			marker := ""
			for _, m := range vanillaMarkers {
				if strings.Contains(line, m) {
					marker = m
					break
				}
			}
			if marker == "" {
				continue
			}
			allowed := false
			for _, a := range vanillaAllow[rel] {
				if strings.Contains(line, a.needle) {
					allowed = true
					used[rel][a.needle] = true
					break
				}
			}
			if !allowed {
				violations = append(violations, rel+": "+strings.TrimSpace(line))
			}
		}
	}
	for _, v := range violations {
		t.Errorf("vanilla JS outside the allowlist (route through Datastar or justify in vanillaAllow): %s", v)
	}
	for rel, needles := range vanillaAllow {
		// Trim-aware: a trimmed tree deletes whole features, so an entry
		// whose file is gone is coverage of nothing, not a stale row. Only
		// present files can carry convertible vanilla.
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			continue
		}
		for _, a := range needles {
			if !used[rel][a.needle] {
				t.Errorf("stale allowlist entry (convert it and delete the row): %s: %s", rel, a.needle)
			}
		}
	}
}
