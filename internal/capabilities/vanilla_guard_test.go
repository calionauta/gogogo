// Vanilla-JS allowlist guard (locality-of-behavior enforcement).
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
	// The transport banner is declarative now (internal/components/
	// offline_banner.templ: Datastar signals, no script), so it needs no
	// allowlist row. Static .js files are not scanned by this guard, which
	// only inspects .templ islands.
	// Drawer focus trap (internal/ui/shell.templ): Datastar owns $railOpen
	// state and every dismissal path (Escape, overlay, destination tap, the
	// collapse button), and the drawer's checkbox mirrors it, but no Datastar
	// primitive cycles Tab inside an open drawer or moves focus across its
	// boundary — the keydown listener plus the open/return globals (called
	// from data-effect / data-on:*) are the island. The ≤1023px matchMedia
	// mirrors chrome.css and daisyUI's lg:drawer-open: on desktop the rail is
	// a persistent column and the trap stays out of the way.
	"internal/ui/shell.templ": {
		{`__gogogoRail.addEventListener("keydown", function (e) {`, "Tab-focus trap"},
		{`window.addEventListener("online", window.__gogogoSyncBadge);`, "badge sync on online"},
		{`window.addEventListener("focus", window.__gogogoSyncBadge);`, "badge sync on focus"},
	},
	// Kiosk order page (features/kiosk/components/page.templ): the mic
	// button drives the browser's own SpeechRecognition, and the only way to
	// start and stop it is an imperative call on the API object. Ruled out:
	// a Datastar signal for "listening" — the recognition session itself is
	// owned by the browser, so the button has to hand the toggle to it. The
	// rest of the island (the watchdog timer, the spoken replies) is the
	// browser API's own lifecycle, not app state.
	//
	// The click is NOT an island: Datastar's data-on:click calls
	// __kioskVoice.toggle (a second listener fired on the same click and the
	// two toggles cancelled out). What remains is the DOM-ready deferral: the
	// component renders into <head>, where getElementById returns null for
	// every body id, and Datastar has no primitive that runs "after the body
	// is parsed" from a <head> script. Ruled out: moving the component to the
	// body — the shell's head slot is the only place a page contributes scripts
	// without a recipe-owned route.
	"features/kiosk/components/page.templ": {
		{
			`document.addEventListener("DOMContentLoaded", initKioskVoice);`,
			"head script waits for the body (no Datastar primitive)",
		},
	},
	// Nav link row: ONE <details> serves the inline row and the collapsed
	// menu, so its `open` state must follow the stylesheet's breakpoint.
	// Ruled out: data-attr:open driven by a $navOpen signal — the breakpoint
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
