// SCOPE:core — the "no fixed naps" guard for the Playwright harnesses.
//
// WHY THIS EXISTS. A fixed sleep is the single most common way a browser test
// becomes flaky: it encodes an assumption about how long a thing takes, which is
// exactly the assumption the CI machine is free to violate. The repo already
// states the principle (commit f3e585a, "wait on events, not fixed naps, in the
// Playwright harnesses") and the harnesses implement it — `waitForInk`,
// `waitForCanvas`, `waitForFunction` all poll state. What was missing is a gate:
// prose in a commit message does not stop the next `waitForTimeout(2000)`.
//
// The concrete incident this guards against is measured, not hypothetical: the
// whiteboard negative-drag assertion sampled canvas ink once, after a wait that
// only proved the peer's cursor dot had moved. It passed with `ink 97` and
// failed with `ink 0` on byte-identical source (failing run 39081962940 vs
// passing 39079507330; `git diff` between them is CSS/docs/CI only).
//
// WHAT IT BANS
//
//   - `page.waitForTimeout(ms)` / `waitForTimeout(ms)`
//   - `await sleep(ms)`
//
// BOTH are subject to the SAME rule, and that is the point: they are one
// primitive under two names, `waitForTimeout` being a thin wrapper over
// `setTimeout`. An earlier draft banned `waitForTimeout` outright and then had to
// carve holes for the two legitimate uses the tree actually contains (a poll
// interval, an absence check) — the shape of a rule that was wrong about the
// world. One threshold, one justification token, both spellings.
//
// AbortSignal-style timeouts (`timeout: N` on a waitFor* call) are NOT this
// primitive and are not flagged: they bound a poll that is genuinely waiting for
// a condition.
//
// # WHAT IT ALLOWS, and why the line is drawn where it is
//
// Short waits (< napThresholdMs) stay legal: they are sequencing, not
// synchronization — "let the click land before reading position", "advance past
// this frame". Banning them would push authors toward something worse, and they
// cannot hide a real race because nothing above ~300ms of protocol latency fits
// inside them.
//
// A LONG wait is legal ONLY with an explicit justification comment on the same
// line: one of the tokens in napJustifications, each of which names a category
// where time itself IS the mechanism being tested. That is the repo's existing
// convention (`await sleep(200); // KEPT: the mid sample instant IS the
// measurement`), now enforced instead of remembered.
//
// REQUIRING THE TOKEN IS THE POINT. A bare `await sleep(1200)` says nothing
// about intent; `// KEPT: lag injected in the route handler` says the sleep IS
// the fixture. There is no file allowlist, because a gate that verifies intent
// beats a gate that whitelists files.
package capabilities

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// napThresholdMs is where a sleep stops being sequencing and starts being an
// attempted synchronization. 500ms matches the repo's own line — the whiteboard
// harness header documents "sub-300ms sequencing" as the allowed band, and every
// sleep below this threshold in the tree is either that or a poll interval.
//
// The threshold is deliberately generous. A gate tuned so tightly that it
// flags `sleep(300)` before mouse.down would force churn on legitimate
// sequencing without catching a single real race: nothing above ~300ms of
// protocol latency was ever hidden inside 300ms.
const napThresholdMs = 500

// napJustifications are the tokens that excuse a LONG sleep, each naming a case
// where wall-clock time IS the mechanism rather than an obstacle to it. `KEPT:`
// is the repo's established marker; the others exist so the reason is legible at
// the call site instead of a bare token.
//
// Three categories are legitimate, and they are the only three:
//
//   - a POLL INTERVAL inside a bounded loop that re-checks a condition (the
//     sleep is the backoff between checks, and the loop carries the deadline);
//   - INJECTED LAG, where a route handler delays on purpose and the delay is the
//     fixture under test;
//   - an ABSENCE CHECK or measured instant, where the window itself is what the
//     assertion is about ("no further reloads once settled", "sample mid-skew").
var napJustifications = []string{
	"KEPT:",             // the repo's existing convention
	"poll interval",     // backoff inside a bounded condition loop
	"lag injected",      // a route handler delays on purpose; the delay IS the fixture
	"time-as-mechanism", // the test measures elapsed time
	"absence check",     // the window bounds a "nothing happens" assertion
	"settle window",     // ditto: proving quiescence requires a window
}

// banAlways existed in an earlier draft and is gone: banning one spelling while
// thresholding the other is incoherent when both are `setTimeout` underneath.
// The single rule below covers both.
//
// napCallRe captures the two spellings of the same primitive and their
// argument. Matching a single line means the justification token must be on that
// line.
var napCallRe = regexp.MustCompile(`(?:await\s+sleep|waitForTimeout)\((\d+)\)`)

func TestNoFixedNapsInBrowserHarnesses(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	var files []string
	for _, dir := range []string{"scripts"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".mjs") {
				return err
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			files = append(files, rel)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(files) == 0 {
		t.Fatal("no .mjs harnesses found under scripts/ — the guard is checking nothing")
	}

	// Read outside the walk callback: gosec G122 forbids filesystem operations
	// on walked paths inside it (symlink TOCTOU) — same reason vanilla_guard does.
	for _, rel := range files {
		raw, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			at := rel + ":" + strconv.Itoa(i+1)
			m := napCallRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			ms, _ := strconv.Atoi(m[1])
			if ms < napThresholdMs {
				continue // sequencing, not synchronization
			}
			if hasJustification(line) {
				continue
			}
			t.Errorf("%s: a %dms fixed wait is an attempted synchronization, not a wait "+
				"(nothing above ~%dms of protocol latency fits inside it). Poll the state "+
				"you actually need — see waitForInk / waitForCanvas / waitForFunction — or, "+
				"if time IS the mechanism, say so on the line with one of %v (%s)",
				at, ms, napThresholdMs, napJustifications, strings.TrimSpace(line))
		}
	}
}

// hasJustification reports whether a line carries one of the documented tokens.
func hasJustification(line string) bool {
	for _, tok := range napJustifications {
		if strings.Contains(line, tok) {
			return true
		}
	}
	return false
}
