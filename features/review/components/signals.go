// SCOPE:feature — Review loop page signals.
package components

import "encoding/json"

// Signals seeds the review page data-signals. The run UI gates on
// signals.ReviewEnabled (registration truth: routes wired AND a real LLM
// configured) — the capability conformance test requires that literal in
// a .templ file, see review.templ.
//
// NOTE: the snake_case JSON keys are intentionally NOT tagliatelle-camel
// — they are public Datastar signal names bound by the frontend, so
// renaming would break the wire contract (same rule as todo.Signals).
type Signals struct {
	ReviewEnabled    bool    `json:"reviewEnabled"`
	ReviewPending    bool    `json:"review_pending"`    //nolint:tagliatelle // public Datastar signal name, do not rename
	ReviewPhase      string  `json:"review_phase"`      //nolint:tagliatelle // public Datastar signal name, do not rename
	ReviewError      string  `json:"review_error"`      //nolint:tagliatelle // public Datastar signal name, do not rename
	ReviewDecision   string  `json:"review_decision"`   //nolint:tagliatelle // public Datastar signal name, do not rename
	ReviewRationale  string  `json:"review_rationale"`  //nolint:tagliatelle // public Datastar signal name, do not rename
	ReviewConfidence float64 `json:"review_confidence"` //nolint:tagliatelle // public Datastar signal name, do not rename
	ReviewRounds     int     `json:"review_rounds"`     //nolint:tagliatelle // public Datastar signal name, do not rename
	ReviewProposal   string  `json:"review_proposal"`   //nolint:tagliatelle // public Datastar signal name, do not rename
}

// MarshalSignals renders Signals for data-signals. Never fails the page:
// unmarshalable input cannot happen from the struct above, so failure
// falls back to an empty object.
func MarshalSignals(s Signals) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}
