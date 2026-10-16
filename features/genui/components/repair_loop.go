// SCOPE:plugin — the bounded validate→repair loop behind both AI legs.
package components

import (
	"context"
	"errors"

	"github.com/calionauta/gogogo/internal/observability"
)

// repairAttempts is how many EXTRA model calls a rejected answer may cost.
// One: the research on catalog-conditioned generation caps the loop at a
// handful of tries, and every extra attempt is a full model round-trip on
// a tab that is already showing a spinner. One retry fixes the common
// failure (a typo-level shape error) without turning a broken prompt into
// a latency storm; the deterministic fallback handles the rest.
const repairAttempts = 1

// repairExcerptCap bounds how much of the rejected answer travels back to
// the model. The failure message names what broke; the payload is context,
// and pasting a long one doubles the cost of the retry for no gain.
const repairExcerptCap = 600

// ParseFunc validates one raw model answer against the catalog.
type ParseFunc func(raw string) ([]Directive, error)

// ValidationError wraps the last catalog failure after the repair loop is
// exhausted. It exists so a caller can tell the two failure modes apart:
// the model answered but never in shape (this), versus the model never
// answered (the responder's own error, which carries the actionable
// provider hint). Collapsing them is how "no API key" and "the model
// returned bad JSON" start reading as the same sentence.
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

// RepairFeedback is appended to the user request on a retry: it names the
// failure and shows the rejected answer, so the model fixes the one thing
// that broke instead of re-rolling the whole answer. Written as an
// instruction to the model, never shown to the user (the user sees either
// the repaired render or the deterministic failure).
//
// Exported for tests and for the chat leg, which reuses the same text over
// its own prompt.
func RepairFeedback(raw string, failure error) string {
	excerpt := raw
	if len(excerpt) > repairExcerptCap {
		excerpt = excerpt[:repairExcerptCap] + "…"
	}
	return "\n\nYour previous answer was REJECTED and not shown to the user. " +
		"The problem: " + failure.Error() + "\n" +
		"The rejected answer was:\n" + excerpt + "\n" +
		"Reply again with a single JSON object that fixes exactly that problem. " +
		"Use ONLY the components named above, with the props they document. " +
		"No prose outside the JSON object."
}

// AnswerWithRepair asks the model, validates through parse, and on failure
// makes up to repairAttempts further attempts with the failure fed back.
// It returns the first directive list that validates, or the last error
// with a nil list — the caller decides how its own surface degrades (Ask
// fails closed and shows the message; chat degrades to prose).
//
// The loop lives here, not in each worker, because both legs have to agree
// on it: an answer that a repair rescued in chat must be rescued in Ask
// too, or the two features disagree about what the model is allowed to
// produce.
func AnswerWithRepair(
	ctx context.Context, r Responder, request string, parse ParseFunc,
) ([]Directive, string, error) {
	if r == nil {
		return nil, "", errors.New("genui: no responder wired")
	}
	raw, err := r.Respond(ctx, request)
	if err != nil {
		return nil, "", err
	}
	dirs, parseErr := parse(raw)
	if parseErr == nil {
		observability.Info(ctx, "genui: catalog answer validated",
			append(observability.WithComponent("genui", "feature"),
				"attempts", 1, "repaired", false, "components", len(dirs))...)
		return dirs, raw, nil
	}
	for attempt := range repairAttempts {
		raw, err = r.Respond(ctx, request+RepairFeedback(raw, parseErr))
		if err != nil {
			return nil, raw, err
		}
		dirs, parseErr = parse(raw)
		if parseErr == nil {
			// Countable from the log pipeline (stdout is JSONL): how often
			// a rejected answer was rescued rather than shown as a failure
			// is the number that says whether the prompt or the catalog is
			// the thing to fix.
			observability.Info(ctx, "genui: catalog answer validated",
				append(observability.WithComponent("genui", "feature"),
					"attempts", attempt+2, "repaired", true, "components", len(dirs))...)
			return dirs, raw, nil
		}
	}
	return nil, raw, &ValidationError{Err: parseErr}
}
