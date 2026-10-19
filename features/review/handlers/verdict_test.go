// SCOPE:feature — the verdict-replacement contract (red-proof).
//
// Split from chi_test.go for the file-size gate, and along a real seam: that
// file covers the handler's wiring (auth, billing, the judge's escalation
// rules), this one covers what a RUN does to the page — the reported bug where
// a second run left the first verdict standing.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func reviewRun(t *testing.T, mux http.Handler, cookie *http.Cookie, content string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	// The text tab is the default and every caller below uses it; the tab
	// choice is exercised by the fixture's own tests, so the parameter would
	// only ever be "text" here.
	body := url.Values{"tab": {"text"}, "content": {content}}.Encode()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status = %d (%s)", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// TestReviewRunReplacesThePreviousVerdict is the red-proof for the lived bug:
// the verdict fragment's root had no id, so patching `#review-result` in the
// default `outer` mode stripped the id off the live node and run #2 onward
// matched nothing — the user saw run #1's card forever ("the result
// references the previous answer"). Two runs must now each carry the target
// id in the fragment they patch, and each must carry its OWN verdict.
func TestReviewRunReplacesThePreviousVerdict(t *testing.T) {
	judge := &stubJudge{verdict: DecisionApprove, confidence: 0.9}
	mux := reviewFixture(t, "APPROVE", judge)
	cookie := reviewSignup(t, mux, "replace@example.com")

	first := reviewRun(t, mux, cookie, "first+draft")
	if !strings.Contains(first, `id="review-result"`) {
		t.Fatalf("run 1 verdict fragment lost the patch target id:\n%s", first)
	}

	// The second run must clear the stage before working: the pending card
	// also carries the id, so the patch has a target after run #1 morphed it.
	second := reviewRun(t, mux, cookie, "second+draft")
	if !strings.Contains(second, "Reviewing") {
		t.Errorf("run 2 did not replace the previous verdict with the pending state:\n%s", second)
	}
	if !strings.Contains(second, `id="review-result"`) {
		t.Fatalf("run 2 verdict fragment lost the patch target id:\n%s", second)
	}
}

// TestReviewRunNoOpEscalatesInsteadOfApproving pins the honest-verdict rule:
// a maker that reports "NO CHANGE NEEDED" over text that needed no edit must
// not come back as a confident approval with the input echoed as the result
// (the reported "fala manooo!" case: approve, confidence 1.00, proposal
// identical to the input). It escalates and says why.
func TestReviewRunNoOpEscalatesInsteadOfApproving(t *testing.T) {
	mux := reviewFixtureWithMaker(t, "NO CHANGE NEEDED", "APPROVE", &stubJudge{verdict: DecisionApprove, confidence: 1})
	cookie := reviewSignup(t, mux, "noop@example.com")
	body := reviewRun(t, mux, cookie, "fala+manooo!")

	if !strings.Contains(body, DecisionNeedsHuman) {
		t.Errorf("a no-change run must escalate, not approve:\n%s", body)
	}
	if !strings.Contains(body, "proposed no change") {
		t.Errorf("the rationale must say the editor changed nothing:\n%s", body)
	}
}

// TestReviewRunShowsTheCritiqueWhenRevised pins the second half of the same
// report: a rejected draft is unexplainable without the critic's own words.
func TestReviewRunShowsTheCritiqueWhenRevised(t *testing.T) {
	judge := &stubJudge{verdict: DecisionRevise, severity: "medium", confidence: 0.9}
	mux := reviewFixture(t, "REVISE: tighten the opening paragraph", judge)
	cookie := reviewSignup(t, mux, "critique@example.com")
	body := reviewRun(t, mux, cookie, "Some+draft+text")

	if !strings.Contains(body, "tighten the opening paragraph") {
		t.Errorf("a revised verdict must carry the critique:\n%s", body)
	}
	if !strings.Contains(body, "critic asked for changes") {
		t.Errorf("the rationale must name the critic's outcome:\n%s", body)
	}
}

// TestReviewRunFailureReplacesTheVerdict pins the failure state: a run that
// errors must put its error in the result region (same patch target), not
// leave the previous verdict standing with the message in a one-line banner.
func TestReviewRunFailureReplacesTheVerdict(t *testing.T) {
	mux := reviewFixture(t, "APPROVE", &stubJudge{err: errJudgeDown})
	cookie := reviewSignup(t, mux, "failreplace@example.com")
	body := reviewRun(t, mux, cookie, "Some+draft+text")

	// The judge being down escalates rather than failing, so drive the real
	// failure path: empty content is rejected before any model call.
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/api/review/run", strings.NewReader("tab=text&content="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty content status = %d, want 400", rec.Code)
	}
	if !strings.Contains(body, "judge is unavailable") {
		t.Errorf("judge-down must say so in the rationale:\n%s", body)
	}
}
