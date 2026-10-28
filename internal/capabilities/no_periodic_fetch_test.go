// SCOPE:core — No periodic server fetch (the poll guard).
//
// The stack pushes: every page already owns a stream (its feature stream, or
// the shared session leader stream on pages without one), and global state
// rides that stream plus a BroadcastChannel. A timer that asks the server for
// state is a poll even when it is dressed as a Datastar primitive, and it has
// a measured cost: Chromium caps HTTP/1.1 connections per host at six, so five
// same-user tabs with a stream each leave no socket for a POST — the notes
// caret suite hung exactly there when the shell briefly opened one more
// stream.
//
// A purely local timer (a clock, an animation, a presence sweep) is
// legitimate: add it to periodicAllow/periodicJSAllow with the primitive you
// ruled out and why, and this test stops flagging it. The stale-entry checks
// keep the lists honest.
package capabilities

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// periodicAllow is empty on purpose: no page currently needs a timer. A new
// entry names the file and the exact line needle, with the local-only reason.
var periodicAllow = map[string][]vanillaAllowed{}

// periodicJSAllow pins the first-party static scripts that own a local timer.
// They are render/presence sweeps, not server-state polls: no file here fetches
// state on an interval. A new standing timer needs the same proof.
var periodicJSAllow = map[string]string{
	"features/notes/static/notes.js":           "caret/typing presence heartbeat + local render/expiry sweeps",
	"features/whiteboard/static/whiteboard.js": "local cursor/draft render and expiry sweeps",
}

// vendoredJS is not first-party code: the pinned Datastar bundle owns its own
// on-interval implementation.
var vendoredJS = map[string]bool{
	"web/resources/static/datastar.js": true,
}

// TestNoPeriodicFetch fails on the poll primitive in markup and on a standing
// timer in first-party static JS. `setInterval` in a .templ is already covered
// by TestNoNewVanillaJS; this covers the Datastar shape that is easy to
// mistake for the framework's blessing.
func TestNoPeriodicFetch(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	used := map[string]map[string]bool{}
	for rel := range periodicAllow {
		used[rel] = map[string]bool{}
	}
	checkTemplIntervals(t, root, used)
	checkStaticTimers(t, root)
	checkStalePeriodicEntries(t, root, used)
}

// checkTemplIntervals flags every data-on-interval outside periodicAllow.
func checkTemplIntervals(t *testing.T, root string, used map[string]map[string]bool) {
	t.Helper()
	for _, rel := range collectTempls(t, root) {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for line := range strings.SplitSeq(string(raw), "\n") {
			if !strings.Contains(line, "data-on-interval") || templIntervalAllowed(rel, line, used) {
				continue
			}
			t.Errorf("periodic fetch in %s: %s\n"+
				"A timer that asks the server for state is a poll. Reuse the page's own SSE stream; "+
				"for shell/global state use the leader session stream (session.js: Web Locks + "+
				"BroadcastChannel) and refresh on visibility/focus/online/pageshow. One stream per "+
				"page is the budget (five same-user tabs already fill the six-per-host cap). "+
				"If this timer is purely local (clock/animation), add it to periodicAllow with that reason.",
				rel, strings.TrimSpace(line))
		}
	}
}

// templIntervalAllowed records one allowlist hit, if the line matches.
func templIntervalAllowed(rel, line string, used map[string]map[string]bool) bool {
	for _, a := range periodicAllow[rel] {
		if !strings.Contains(line, a.needle) {
			continue
		}
		used[rel][a.needle] = true
		return true
	}
	return false
}

func collectTempls(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	for _, dir := range []string{"features", "web", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".templ") {
				return err
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			paths = append(paths, rel)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return paths
}

// checkStaticTimers flags first-party static JS with a standing interval
// outside the allowlist.
func checkStaticTimers(t *testing.T, root string) {
	t.Helper()
	for _, dir := range []string{"web/resources/static", "features"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".js") {
				return err
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			if vendoredJS[rel] || strings.HasSuffix(rel, ".min.js") {
				return nil
			}
			raw, rerr := os.ReadFile(filepath.Join(root, rel))
			if rerr != nil {
				t.Fatalf("read %s: %v", rel, rerr)
			}
			if !strings.Contains(string(raw), "setInterval(") {
				return nil
			}
			if _, ok := periodicJSAllow[rel]; !ok {
				t.Errorf("standing timer in %s — a periodic callback is a poll unless it is a local-only "+
					"render/presence sweep. Reuse the page stream or session.js (leader + BroadcastChannel); "+
					"never fetch on an interval. If this is local-only, add the file to periodicJSAllow with "+
					"the reason (and no fetch).", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

func checkStalePeriodicEntries(t *testing.T, root string, used map[string]map[string]bool) {
	t.Helper()
	for rel, needles := range periodicAllow {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			continue // trimmed tree: the file is gone, not a stale entry
		}
		for _, a := range needles {
			if !used[rel][a.needle] {
				t.Errorf("stale periodicAllow entry (remove or justify): %s: %s", rel, a.needle)
			}
		}
	}
	for rel := range periodicJSAllow {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			continue // trimmed tree
		}
		raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if !strings.Contains(string(raw), "setInterval(") {
			t.Errorf("stale periodicJSAllow entry (the timer is gone; delete the row): %s", rel)
		}
	}
}
