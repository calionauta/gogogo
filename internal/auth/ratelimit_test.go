// SCOPE:core — attempt-throttle contract tests.
package auth

import (
	"testing"
	"time"
)

// TestAttemptTrackerBudget pins the shared machinery both throttles run
// on: burst then fail-closed, sliding window recovery, success reset.
// Delete the window pruning and the burst never recovers; delete forget
// and one typo burst poisons the next login.
func TestAttemptTrackerBudget(t *testing.T) {
	var tr attemptTracker
	for range attemptMax {
		if !tr.allow("k") {
			t.Fatal("blocked inside budget")
		}
	}
	if tr.allow("k") {
		t.Fatal("attempt past budget allowed")
	}
	tr.forget("k")
	if !tr.allow("k") {
		t.Fatal("blocked after forget (success must reset)")
	}
}

// TestAttemptTrackerWindowSlides pins recovery without success: failures
// older than the window stop counting.
func TestAttemptTrackerWindowSlides(t *testing.T) {
	tr := attemptTracker{max: 2, window: time.Minute}
	for range 2 {
		if !tr.allow("k") {
			t.Fatal("inside budget blocked")
		}
	}
	if tr.allow("k") {
		t.Fatal("past budget allowed")
	}
	// Age every hit past the window, then one more attempt must pass.
	tr.mu.Lock()
	aged := tr.hits["k"][:0]
	for range tr.hits["k"] {
		aged = append(aged, time.Now().Add(-2*time.Minute))
	}
	tr.hits["k"] = aged
	tr.mu.Unlock()
	if !tr.allow("k") {
		t.Fatal("stale failures still count past the window")
	}
}

// TestAttemptTrackerCustomBudget pins the signin override: nonzero max
// replaces the default instead of adding to it.
func TestAttemptTrackerCustomBudget(t *testing.T) {
	tr := attemptTracker{max: signinAttemptMax}
	max, window := tr.budget()
	if max != signinAttemptMax || window != attemptWindow {
		t.Fatalf("budget = %d/%v, want %d/%v", max, window, signinAttemptMax, attemptWindow)
	}
	var zero attemptTracker
	max, _ = zero.budget()
	if max != attemptMax {
		t.Fatalf("zero-value budget = %d, want default %d", max, attemptMax)
	}
}
