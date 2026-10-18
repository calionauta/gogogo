// SCOPE:core — the quarantine mechanism's own contract.
//
// Mostly about the DEADLINE, because that is the part that makes a quarantine
// honest: without an expiry it is a deletion that lies about itself. The date
// logic is driven directly (an injected `now`) rather than by registering
// entries and waiting for a clock.
//
// The validator is tested through `validateQuarantines` with INJECTED entries,
// not through the shipped registry: asserting against the real map would mean
// either mutating global state or asserting nothing the moment the map is empty
// (which is its correct state).
package hostgate

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestQuarantineExpiresAtTheStartOfTheFollowingDay pins the boundary. An
// off-by-one-day expiry is invisible until it matters: lapsing a day early
// nags, lapsing a day late keeps a test skipped with nobody the wiser.
func TestQuarantineExpiresAtTheStartOfTheFollowingDay(t *testing.T) {
	t.Parallel()
	const day = "2026-03-10"
	cases := map[string]struct {
		now  string
		want bool
	}{
		"the day itself is still valid":     {"2026-03-10T00:00:00Z", false},
		"later that day is still valid":     {"2026-03-10T23:59:59Z", false},
		"the next day is expired":           {"2026-03-11T00:00:01Z", true},
		"well past is expired":              {"2026-06-01T12:00:00Z", true},
		"the day before is not yet expired": {"2026-03-09T23:00:00Z", false},
	}
	for name, tc := range cases {
		now, err := time.Parse(time.RFC3339, tc.now)
		if err != nil {
			t.Fatalf("%s: parse now: %v", name, err)
		}
		got, err := quarantineExpired(day, now)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != tc.want {
			t.Errorf("%s: now=%s expires=%s -> expired=%v, want %v",
				name, tc.now, day, got, tc.want)
		}
	}
}

// TestQuarantineRejectsAMalformedDate is the fail-open case: a typo must not
// silently keep a test skipped, because "skipped" is indistinguishable from
// "passing" in a summary. Relative deadlines are rejected too — they move every
// time someone touches the entry, which is how a quarantine becomes permanent.
func TestQuarantineRejectsAMalformedDate(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "next month", "2026-3-1", "10/03/2026", "+30d"} {
		if _, err := quarantineExpired(bad, time.Now()); err == nil {
			t.Errorf("expiry %q parsed, want an error (use %s)", bad, expiryLayout)
		}
	}
}

// TestShippedQuarantineRegistryIsValid pins the correct default: an empty
// registry, or every entry well formed and unexpired.
func TestShippedQuarantineRegistryIsValid(t *testing.T) {
	t.Parallel()
	if errs := ValidateQuarantines(time.Now()); len(errs) != 0 {
		t.Errorf("the shipped quarantine registry has %d problem(s):", len(errs))
		for _, e := range errs {
			t.Errorf("  %v", e)
		}
	}
}

// TestValidateQuarantinesCatchesBadEntries exercises all three checks with
// injected entries, so it keeps meaning even while the registry is empty.
func TestValidateQuarantinesCatchesBadEntries(t *testing.T) {
	t.Parallel()
	now, _ := time.Parse(time.RFC3339, "2026-06-01T00:00:00Z")
	good := quarantine{
		reason:  "canvas paint races the cursor DOM node",
		issue:   "https://example.invalid/1",
		expires: "2026-12-31",
	}

	if errs := validateQuarantines(map[string]quarantine{"ok": good}, now); len(errs) != 0 {
		t.Errorf("a valid entry was rejected: %v", errs)
	}

	bad := map[string]struct {
		q    quarantine
		want string
	}{
		"no reason":  {quarantine{issue: good.issue, expires: good.expires}, "no reason"},
		"no issue":   {quarantine{reason: good.reason, expires: good.expires}, "no issue"},
		"expired":    {quarantine{reason: good.reason, issue: good.issue, expires: "2026-01-01"}, "EXPIRED"},
		"bad expiry": {quarantine{reason: good.reason, issue: good.issue, expires: "soon"}, "not 2006-01-02"},
		"whitespace reason": {
			quarantine{reason: "   ", issue: good.issue, expires: good.expires}, "no reason"},
	}
	for name, tc := range bad {
		errs := validateQuarantines(map[string]quarantine{name: tc.q}, now)
		if len(errs) == 0 {
			t.Errorf("%s: accepted, want a validation error mentioning %q", name, tc.want)
			continue
		}
		found := false
		for _, e := range errs {
			if contains(e.Error(), tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: errors %v do not mention %q", name, errs, tc.want)
		}
	}
}

// TestQuarantineIsANoOpForUnregisteredTests is the property that keeps this
// helper safe to add to any test: for everyone not in the registry it must run
// the body and never skip or modify state.
func TestQuarantineIsANoOpForUnregisteredTests(t *testing.T) {
	t.Parallel()
	body := false
	got := Quarantine(t, "definitely-not-a-registered-quarantine-name")
	body = true
	if !got || !body {
		t.Fatalf("Quarantine returned %v for an unregistered name, want true", got)
	}
	if t.Skipped() {
		t.Fatal("Quarantine skipped an unregistered test")
	}
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// recordingTB is a testing.TB that RECORDS the calls Quarantine makes instead of
// acting on them. It is what makes both branches assertable in one test.
//
// It exists because of a behavior worth knowing: `tb.Skipf` calls
// `runtime.Goexit()`, so it TERMINATES the test function rather than merely
// marking it — a test that skips is dead from that line, which is why the
// documented call site is `if !Quarantine(...) { return }` (the return reads
// well; the runtime does not need it). With the real *testing.T there is no way
// to assert "it skipped with this message" and then keep going.
//
// `testing.TB` has an unexported method, so an outside type cannot implement it
// from scratch — but EMBEDDING the interface satisfies it, and only the four
// methods Quarantine calls are overridden. Any other call would panic on the nil
// embedded value, which is a feature: it means Quarantine's surface is pinned by
// this fake.
type recordingTB struct {
	testing.TB
	errors []string
	skips  []string
	logs   []string
}

func (r *recordingTB) Helper()                   {}
func (r *recordingTB) Errorf(f string, a ...any) { r.errors = append(r.errors, sprintf(f, a...)) }
func (r *recordingTB) Skipf(f string, a ...any)  { r.skips = append(r.skips, sprintf(f, a...)) }
func (r *recordingTB) Logf(f string, a ...any)   { r.logs = append(r.logs, sprintf(f, a...)) }

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }

// TestQuarantineBranches pins all five outcomes at once, which the real
// *testing.T cannot do (see recordingTB). Each case asserts the CALL, not a
// return value alone, so a branch that fires for the wrong reason is caught.
func TestQuarantineBranches(t *testing.T) {
	// NOT parallel: mutates the shared registry.
	const name = "TestFixture_QuarantinedInThisTest"
	t.Cleanup(func() { delete(quarantined, name) })
	future := time.Now().AddDate(1, 0, 0).Format(expiryLayout)
	good := func(expires string) quarantine {
		return quarantine{
			reason:  "fixture: exercises one branch",
			issue:   "https://example.invalid/fixture",
			expires: expires,
		}
	}

	t.Run("unregistered is a no-op", func(t *testing.T) {
		delete(quarantined, name)
		tb := &recordingTB{}
		if got := Quarantine(tb, name); !got {
			t.Error("Quarantine returned false for an unregistered name, want true")
		}
		if len(tb.skips)+len(tb.errors)+len(tb.logs) != 0 {
			t.Errorf("a no-op called something: skips=%v errors=%v logs=%v",
				tb.skips, tb.errors, tb.logs)
		}
	})

	t.Run("live quarantine skips with reason, issue and date", func(t *testing.T) {
		quarantined[name] = good(future)
		tb := &recordingTB{}
		if got := Quarantine(tb, name); got {
			t.Error("Quarantine returned true for a live quarantine, want false (skip)")
		}
		if len(tb.skips) != 1 {
			t.Fatalf("want exactly one Skipf, got %d (%v)", len(tb.skips), tb.skips)
		}
		s := tb.skips[0]
		for _, want := range []string{future, "fixture: exercises one branch", "https://example.invalid/fixture"} {
			if !strings.Contains(s, want) {
				t.Errorf("skip message omits %q: %s", want, s)
			}
		}
		if len(tb.errors) != 0 {
			t.Errorf("a live quarantine reported an error: %v", tb.errors)
		}
	})

	t.Run("expired quarantine runs and says so", func(t *testing.T) {
		quarantined[name] = good("2020-01-01")
		tb := &recordingTB{}
		if got := Quarantine(tb, name); !got {
			t.Error("Quarantine returned false for an EXPIRED entry, want true (run)")
		}
		if len(tb.skips) != 0 {
			t.Errorf("an expired quarantine skipped: %v", tb.skips)
		}
		if len(tb.logs) != 1 || !strings.Contains(tb.logs[0], "EXPIRED") {
			t.Errorf("expected one log naming the expiry, got %v", tb.logs)
		}
	})

	t.Run("report mode runs a live quarantine", func(t *testing.T) {
		t.Setenv("QUARANTINE_REPORT", "1")
		quarantined[name] = good(future)
		tb := &recordingTB{}
		if got := Quarantine(tb, name); !got {
			t.Error("Quarantine returned false under QUARANTINE_REPORT=1, want true (run)")
		}
		if len(tb.skips) != 0 {
			t.Errorf("report mode skipped: %v", tb.skips)
		}
		if len(tb.logs) != 1 || !strings.Contains(tb.logs[0], "REPORT mode") {
			t.Errorf("expected one log naming report mode, got %v", tb.logs)
		}
	})

	t.Run("malformed expiry fails open with an error", func(t *testing.T) {
		quarantined[name] = good("someday")
		tb := &recordingTB{}
		if got := Quarantine(tb, name); !got {
			t.Error("Quarantine returned false for an unparseable expiry, want true (fail open)")
		}
		if len(tb.skips) != 0 {
			t.Errorf("a malformed expiry skipped the test (silent coverage loss): %v", tb.skips)
		}
		if len(tb.errors) != 1 {
			t.Errorf("want one error naming the bad expiry, got %v", tb.errors)
		}
	})
}
