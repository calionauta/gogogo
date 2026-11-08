// Fallback judge behaviour tests: primary THX-02, fallback SimpleJev.
package handlers

import (
	"context"
	"errors"
	"testing"
)

// errFallbackDown simulates an unreachable fallback backend.
var errFallbackDown = errors.New("review: fallback backend unreachable")

// TestFallbackJudge pins the primary→fallback chain: a primary success
// never touches the fallback; a primary failure is answered by the
// fallback; when both fail the error preserves both causes. Red-proof:
// make the chain return the primary error directly and the middle case
// goes red.
func TestFallbackJudge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Primary answers → fallback untouched.
	primaryOK := &stubJudge{verdict: "approve", confidence: 0.9}
	fallbackIdle := &stubJudge{verdict: "revise", confidence: 0.8}
	resp, err := (&fallbackJudge{primary: primaryOK, fallback: fallbackIdle}).Evaluate(ctx, "state", nil)
	if err != nil {
		t.Fatalf("primary success returned error: %v", err)
	}
	if got := resp.Answers["verdict"].Choice; got != "approve" {
		t.Errorf("verdict = %q, want primary %q", got, "approve")
	}
	if fallbackIdle.calls != 0 {
		t.Errorf("fallback calls = %d, want 0 when primary answers", fallbackIdle.calls)
	}

	// Primary down → fallback answers.
	primaryDown := &stubJudge{err: errJudgeDown}
	fallbackOK := &stubJudge{verdict: "revise", confidence: 0.8}
	resp, err = (&fallbackJudge{primary: primaryDown, fallback: fallbackOK}).Evaluate(ctx, "state", nil)
	if err != nil {
		t.Fatalf("fallback success returned error: %v", err)
	}
	if got := resp.Answers["verdict"].Choice; got != "revise" {
		t.Errorf("verdict = %q, want fallback %q", got, "revise")
	}
	if fallbackOK.calls != 1 {
		t.Errorf("fallback calls = %d, want 1 after primary failure", fallbackOK.calls)
	}

	// Both down → one error naming both causes.
	primaryDown2 := &stubJudge{err: errJudgeDown}
	fallbackDown := &stubJudge{err: errFallbackDown}
	resp, err = (&fallbackJudge{primary: primaryDown2, fallback: fallbackDown}).Evaluate(ctx, "state", nil)
	if err == nil {
		t.Fatal("both legs down returned nil error, want one")
	}
	if !errors.Is(err, errJudgeDown) {
		t.Errorf("error %v does not preserve primary cause", err)
	}
	if !errors.Is(err, errFallbackDown) {
		t.Errorf("error %v does not preserve fallback cause", err)
	}
	if resp != nil {
		t.Errorf("both legs down returned response %v, want nil", resp)
	}
}
