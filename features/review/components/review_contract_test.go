// SCOPE:feature — Review patch-target contract (red-proof).
//
// The bug these tests exist for: the handler patches `#review-result` with
// Datastar's default `outer` mode, which morphs the target and SYNCS ITS
// ATTRIBUTES FROM THE FRAGMENT. A fragment whose root element is not the
// target strips the id off the live node, and every later run then matches
// nothing (PatchElementsNoTargetsFound in the browser console) while the
// previous verdict stays on screen. That read to a user as "the result never
// changes", so the contract is pinned here at the markup level rather than
// left to the browser.
package components

import (
	"strings"
	"testing"
)

// renderResult renders one verdict and fails the test on a render error.
func renderResult(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	err := ReviewResult("approve", "the rationale", "the proposal", "", "approved", 0.9, 2, false).Render(t.Context(), &b)
	if err != nil {
		t.Fatalf("render ReviewResult: %v", err)
	}
	return b.String()
}

// TestReviewResultCarriesThePatchTargetID is the red-proof for the lived bug.
// Drop the id from the component's root and this fails — which is exactly
// what happened in the browser.
func TestReviewResultCarriesThePatchTargetID(t *testing.T) {
	got := renderResult(t)
	if !strings.Contains(got, `id="review-result"`) {
		t.Fatalf("ReviewResult root must carry id=\"review-result\" (the patch target):\n%s", got)
	}
}

// TestReviewPendingCarriesThePatchTargetID pins the same contract for the
// run's opening state: the pending patch replaces the previous verdict, so it
// must be able to find it — and must leave a target behind for the answer.
func TestReviewPendingCarriesThePatchTargetID(t *testing.T) {
	var b strings.Builder
	if err := ReviewPending().Render(t.Context(), &b); err != nil {
		t.Fatalf("render ReviewPending: %v", err)
	}
	if !strings.Contains(b.String(), `id="review-result"`) {
		t.Fatalf("ReviewPending must carry the patch target id:\n%s", b.String())
	}
}

// TestReviewErrorCarriesThePatchTargetID pins it for the failure state too:
// without the id, a failed run both stranded the old card AND left nothing
// for the next run to patch.
func TestReviewErrorCarriesThePatchTargetID(t *testing.T) {
	var b strings.Builder
	if err := ReviewError("boom").Render(t.Context(), &b); err != nil {
		t.Fatalf("render ReviewError: %v", err)
	}
	if !strings.Contains(b.String(), `id="review-result"`) {
		t.Fatalf("ReviewError must carry the patch target id:\n%s", b.String())
	}
}

// TestReviewResultShowsTheCritiqueWhenRevised pins the honest-result half of
// the same report: a verdict whose critic asked for changes must show the
// critique, because that text is the only explanation of WHY the judge
// rejected the draft. Show it unconditionally and the approved case goes red
// below; drop it and this goes red.
func TestReviewResultShowsTheCritiqueWhenRevised(t *testing.T) {
	var revised strings.Builder
	err := ReviewResult("revise", "judge rationale", "the proposal",
		"REVISE: tighten the opening", "revised", 0.9, 1, false).Render(t.Context(), &revised)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(revised.String(), "REVISE: tighten the opening") {
		t.Errorf("a revised verdict must show the critique:\n%s", revised.String())
	}

	approved := renderResult(t)
	if strings.Contains(approved, "Critique") {
		t.Errorf("an approved verdict has no critique to show:\n%s", approved)
	}
}
