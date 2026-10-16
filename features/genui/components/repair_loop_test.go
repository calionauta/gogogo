// SCOPE:plugin — the bounded validate→repair loop's contract.
package components

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// scriptedResponder answers with one canned reply per call, so a test can
// say exactly what the model produced on the first try and after being
// told what broke. It never touches a network or a model.
type scriptedResponder struct {
	replies []string
	errs    []error
	calls   int
	prompts []string
}

func (s *scriptedResponder) Respond(_ context.Context, prompt string) (string, error) {
	s.prompts = append(s.prompts, prompt)
	i := s.calls
	s.calls++
	if i < len(s.errs) && s.errs[i] != nil {
		return "", s.errs[i]
	}
	if i < len(s.replies) {
		return s.replies[i], nil
	}
	return "", errors.New("scriptedResponder: out of replies")
}

const goodAnswer = `{"components":[{"type":"text_note","props":{"text":"ok"}}]}`

// TestAnswerWithRepairFixesARejectedAnswer is the feature: a first answer
// the catalog rejects costs one more model call carrying the failure, and
// the repaired answer renders. Red-proof: return after the first parse
// failure and this fails with an empty directive list.
func TestAnswerWithRepairFixesARejectedAnswer(t *testing.T) {
	t.Parallel()
	bad := `{"components":[{"type":"nope"}]}`
	r := &scriptedResponder{replies: []string{bad, goodAnswer}}
	dirs, raw, err := AnswerWithRepair(context.Background(), r, "plan my day", ParseDirectives)
	if err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		t.Errorf("dirs = %+v, want the repaired text_note", dirs)
	}
	if raw != goodAnswer {
		t.Errorf("raw = %q, want the repaired answer", raw)
	}
	if r.calls != 2 {
		t.Errorf("model calls = %d, want 2 (the answer plus one repair)", r.calls)
	}
	// The retry has to say WHY: a bare re-ask is a re-roll, not a repair.
	if !strings.Contains(r.prompts[1], "REJECTED") || !strings.Contains(r.prompts[1], "nope") {
		t.Errorf("repair prompt does not carry the failure:\n%s", r.prompts[1])
	}
}

// TestAnswerWithRepairIsBounded pins the cost bound: a model that keeps
// answering out of catalog stops after repairAttempts extra calls instead
// of looping, and the failure arrives as a ValidationError so a caller can
// tell it from a dead provider.
func TestAnswerWithRepairIsBounded(t *testing.T) {
	t.Parallel()
	bad := `{"components":[{"type":"nope"}]}`
	r := &scriptedResponder{replies: []string{bad, bad, bad}}
	dirs, raw, err := AnswerWithRepair(context.Background(), r, "x", ParseDirectives)
	if dirs != nil {
		t.Errorf("dirs = %+v, want nil after exhausted repair", dirs)
	}
	if raw != bad {
		t.Errorf("raw = %q, want the last rejected answer", raw)
	}
	if r.calls != repairAttempts+1 {
		t.Errorf("model calls = %d, want %d", r.calls, repairAttempts+1)
	}
	if _, invalid := errors.AsType[*ValidationError](err); !invalid {
		t.Errorf("err = %v, want a *ValidationError", err)
	}
}

// TestAnswerWithRepairKeepsProviderErrorsDistinct is the other half of the
// contract: a responder failure is NOT a validation failure, so Ask can
// still show the provider hint ("no LLM configured") instead of blaming
// the model's JSON.
func TestAnswerWithRepairKeepsProviderErrorsDistinct(t *testing.T) {
	t.Parallel()
	want := errors.New("no LLM configured (GOAI_API_KEY or SIMULATE_LLM=true)")
	r := &scriptedResponder{errs: []error{want}}
	_, _, err := AnswerWithRepair(context.Background(), r, "x", ParseDirectives)
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the provider error", err)
	}
	if _, invalid := errors.AsType[*ValidationError](err); invalid {
		t.Error("a provider failure was reported as a validation failure")
	}
	if r.calls != 1 {
		t.Errorf("model calls = %d, want 1 (no repair after a transport failure)", r.calls)
	}
}

// TestRepairFeedbackBoundsTheExcerpt keeps the retry from doubling its own
// cost: the failure is named in full, the rejected payload is excerpted.
func TestRepairFeedbackBoundsTheExcerpt(t *testing.T) {
	t.Parallel()
	got := RepairFeedback(strings.Repeat("x", repairExcerptCap*2), errors.New("boom"))
	if !strings.Contains(got, "boom") {
		t.Error("feedback does not name the failure")
	}
	if len(got) > repairExcerptCap*2 {
		t.Errorf("feedback is %d bytes, want the excerpt capped", len(got))
	}
}
