// SCOPE:core — Quarantine for tests that are known-flaky, with a deadline.
//
// WHY. A flaky test has three possible fates, and only one of them is honest:
//
//  1. Fix it. Always the first move; see docs/ci-triage.md for how to tell a
//     flake from a real regression.
//  2. Delete it. Sometimes correct — a test that cannot be made deterministic
//     and guards nothing is worse than no test.
//  3. Quarantine it. For the case in between: the test guards something real,
//     the flake is understood, and neither the fix nor the delete is happening
//     this week. This file is only for case 3.
//
// THE AWKWARD FACT THIS DESIGN WORKS AROUND. Go has no native "run this and
// report but do not fail": a failing subtest marks its PARENT failed too
// (measured — `t.Run` with a failing child leaves `parent.Failed() == true`
// before `t.Run` even returns), so wrapping a flaky body in a subtest cannot
// swallow the verdict. And `t.Skip` does not run the body at all, so it throws
// the signal away. Those two facts leave one shape:
//
//   - DEFAULT: a quarantined test SKIPS, loudly — naming the reason, the issue
//     and the expiry — so the gate is not blocked by a known flake and nobody
//     has to guess why the test did not run.
//   - QUARANTINE_REPORT=1: a quarantined test RUNS and reports normally, so the
//     signal is still available without gating anything.
//
// THE DEADLINE IS THE POINT. A quarantine without an expiry is a deletion that
// lies about itself: the test stays in the tree, stops being run, and nobody
// notices it no longer guards anything. A quarantine whose deadline has passed
// runs NORMALLY and therefore fails — so the first CI run after the deadline is
// a reminder that cannot be missed, and `ValidateQuarantines` (called by a
// capabilities conformance test) makes it a red BUILD rather than a surprise.
//
// A separate, measured argument for keeping quarantines short: Google reported
// that when a previously-stable test turned flaky and was traced to a specific
// change, it was a real production bug roughly one time in six. That is a blog
// comment rather than a paper, so treat it as directional — but it is why this
// file carries a loud reason and an issue link instead of a silent ignore list.
package hostgate

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// expiryLayout is the format every expiry is written in: an absolute date, not
// a duration. A relative deadline ("+30d") would move every time someone touched
// the entry, which is how a quarantine becomes permanent.
const expiryLayout = "2006-01-02"

// quarantine records one known-flaky test and what it is waiting for. Unexported
// so the only way in is the registry below, and the only way out is
// `Quarantine`/`ValidateQuarantines` — an entry cannot be conjured at a call
// site with a missing reason.
type quarantine struct {
	// reason is why the test flakes, in one sentence. "it's flaky" is not a
	// reason; "the canvas paint races the cursor DOM node" is.
	reason string
	// issue is the tracking link. Required: a quarantine with nowhere to look
	// is a deletion with extra steps.
	issue string
	// expires is the date (expiryLayout) after which the test runs normally and
	// fails again. Required, and deliberately short — see the package comment.
	expires string
}

// quarantined is the registry. EMPTY is the correct state: every entry is a test
// that is not currently protecting anything.
//
// Adding one requires a reason, an issue and a future expiry, and
// `ValidateQuarantines` (asserted by a capabilities conformance test) fails the
// build on a malformed or expired entry — so this map cannot quietly become a
// graveyard.
var quarantined = map[string]quarantine{}

// Quarantine is called as the first statement of a test that may be quarantined.
// It returns true when the caller should proceed with the test body.
//
// Behavior, in the order the conditions matter:
//   - not registered: run. (The normal case — this must be a no-op for everyone
//     else.)
//   - registered and EXPIRED: run, with a loud log line. It fails if it is still
//     flaky, which is the reminder the deadline exists to produce.
//   - registered, not expired, QUARANTINE_REPORT=1: run and report normally.
//   - registered, not expired, otherwise: SKIP naming reason, issue and expiry.
//
// The registered-but-expired case deliberately does NOT error here: the test
// body is the thing that decides, and a validator already fails the build. Two
// mechanisms for one fact would make the failure message worse, not better.
func Quarantine(tb testing.TB, name string) bool {
	tb.Helper()
	q, ok := quarantined[name]
	if !ok {
		return true
	}
	expired, err := quarantineExpired(q.expires, time.Now())
	if err != nil {
		// A malformed date must not keep the test skipped: "skipped" is
		// indistinguishable from "passing" in a summary, so failing open here
		// and loudly is the only safe direction. The validator also catches it.
		tb.Errorf("hostgate: quarantine %q has an unparseable expiry %q (%v) — "+
			"expected %s; fix the registry entry", name, q.expires, err, expiryLayout)
		return true
	}
	if expired {
		tb.Logf("hostgate: quarantine %q EXPIRED on %s — running the test for real "+
			"(reason was: %s; issue: %s). If it flakes, fix it or delete it; do not "+
			"extend the date without saying why.", name, q.expires, q.reason, q.issue)
		return true
	}
	if os.Getenv("QUARANTINE_REPORT") == "1" {
		tb.Logf("hostgate: quarantine %q in REPORT mode (expires %s) — running so the "+
			"signal is visible; a failure here does not block until %s. Reason: %s. Issue: %s",
			name, q.expires, q.expires, q.reason, q.issue)
		return true
	}
	tb.Skipf("hostgate: QUARANTINED until %s — %s (issue: %s). Run with "+
		"QUARANTINE_REPORT=1 to see it execute, or fix the cause and delete the "+
		"registry entry (see docs/ci-triage.md)",
		q.expires, q.reason, q.issue)
	return false
}

// Quarantined names every registered entry, sorted, so an operator can ask what
// is currently quarantined.
func Quarantined() []string {
	names := make([]string, 0, len(quarantined))
	for name := range quarantined {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ValidateQuarantines checks the shipped registry. It is what the capabilities
// conformance test calls, so a bad entry is a red build.
func ValidateQuarantines(now time.Time) []error {
	return validateQuarantines(quarantined, now)
}

// validateQuarantines takes the registry as a parameter so the checks are
// testable with INJECTED entries — testing them against the shipped map would
// mean either mutating global state or asserting nothing once the map is empty.
func validateQuarantines(reg map[string]quarantine, now time.Time) []error {
	var errs []error
	names := make([]string, 0, len(reg))
	for name := range reg {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		q := reg[name]
		if strings.TrimSpace(q.reason) == "" {
			errs = append(errs, fmt.Errorf("quarantine %q has no reason — say WHY it "+
				"flakes; \"it's flaky\" is a category, not a cause", name))
		}
		if strings.TrimSpace(q.issue) == "" {
			errs = append(errs, fmt.Errorf("quarantine %q has no issue link — a "+
				"quarantine with nowhere to look is a deletion that lies about itself", name))
		}
		expired, err := quarantineExpired(q.expires, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("quarantine %q: expiry %q is not %s: %w",
				name, q.expires, expiryLayout, err))
			continue
		}
		if expired {
			errs = append(errs, fmt.Errorf("quarantine %q EXPIRED on %s — fix the test "+
				"and delete the entry, or delete the test (reason was: %s; issue: %s)",
				name, q.expires, q.reason, q.issue))
		}
	}
	return errs
}

// quarantineExpired reports whether the deadline has passed. It takes `now` so
// the boundary is testable without waiting for a date.
func quarantineExpired(expires string, now time.Time) (bool, error) {
	when, err := time.Parse(expiryLayout, expires)
	if err != nil {
		return false, err
	}
	// The whole named day is still valid; the quarantine lapses at the START of
	// the following day, which is what a date (rather than a timestamp) reads as
	// to the person who wrote it.
	return now.After(when.AddDate(0, 0, 1)), nil
}
