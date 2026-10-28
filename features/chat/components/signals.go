// SCOPE:feature — chat page signals.
package components

import "encoding/json"

// Signals seeds the chat page data-signals. ChatEmpty drives the
// first-run guidance: the composer flips it off on the first submit, so
// the guidance leaves the column without a patch (the appended bubble
// would otherwise sit under a "Ask anything" placeholder).
//
// ChatReady gates the composer: false until the stream handshake lands, so
// a question can never be enqueued before the tab that must receive its
// answer is registered. ChatWorking names the current stage inside the
// thinking bubble ("Thinking…" by default, "Loading your data…" while the
// worker hydrates panels) — the per-answer status, cleared with the answer.
// The answer-vocabulary toggles below the composer are
// plain form fields (one `surfaces` value per enabled band: the official
// surface names catalog, custom, native) — not signals — so unchecking all
// three posts a present-but-empty set (prose-only) with no JS involved.
//
// NOTE: the snake_case JSON keys are intentionally NOT tagliatelle-camel
// — they are public Datastar signal names bound by the frontend, so
// renaming would break the wire contract (same rule as review.Signals).
type Signals struct {
	Theme        string `json:"theme"`
	ChatThinking bool   `json:"chat_thinking"` //nolint:tagliatelle // public Datastar signal name, do not rename
	ChatError    string `json:"chat_error"`    //nolint:tagliatelle // public Datastar signal name, do not rename
	ChatEmpty    bool   `json:"chat_empty"`    //nolint:tagliatelle // public Datastar signal name, do not rename
	ChatReady    bool   `json:"chat_ready"`    //nolint:tagliatelle // public Datastar signal name, do not rename
	ChatWorking  string `json:"chat_working"`  //nolint:tagliatelle // public Datastar signal name, do not rename
	// ChatMessages is the thread's bubble count, seeded from the server-rendered
	// history and stepped by each appended fragment. Without it the demo panel
	// printed the page-load count forever (a live answer sat in the thread while
	// the stat still said 0).
	ChatMessages int `json:"chat_messages"` //nolint:tagliatelle // public Datastar signal name, do not rename
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
