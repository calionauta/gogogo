// SCOPE:feature — chat page signals.
package components

import "encoding/json"

// Signals seeds the chat page data-signals. ChatEmpty drives the
// first-run guidance: the composer flips it off on the first submit, so
// the guidance leaves the column without a patch (the appended bubble
// would otherwise sit under a "Ask anything" placeholder).
//
// NOTE: the snake_case JSON keys are intentionally NOT tagliatelle-camel
// — they are public Datastar signal names bound by the frontend, so
// renaming would break the wire contract (same rule as review.Signals).
type Signals struct {
	Theme        string `json:"theme"`
	ChatThinking bool   `json:"chat_thinking"` //nolint:tagliatelle // public Datastar signal name, do not rename
	ChatError    string `json:"chat_error"`    //nolint:tagliatelle // public Datastar signal name, do not rename
	ChatEmpty    bool   `json:"chat_empty"`    //nolint:tagliatelle // public Datastar signal name, do not rename
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
