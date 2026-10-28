// SCOPE:feature — the todo panel's filter tolerance (red-proof).
package todo

import "testing"

// TestNormalizeFilter_AcceptsWhatModelsWrite is the red-proof for a dropped
// panel: the model answered `filter: "ALL"` (and other near-misses the prompt
// never mentions), validation rejected it, Hydrate replaced the panel, and the
// user saw a hole where their todos should be. Every accepted spelling here is
// one a model is known to produce for an enum it was given in lowercase.
func TestNormalizeFilter_AcceptsWhatModelsWrite(t *testing.T) {
	cases := map[string]string{
		"open": "open", "OPEN": "open", " open ": "open", "todo": "open",
		"todos": "open", "pending": "open", "active": "open", "not done": "open",
		"done": "done", "DONE": "done", "completed": "done", "Complete": "done",
		"finished": "done", "all": "all", "ALL": "all", "Everything": "all",
		"": "open", // omitted filter means "what is still to do"
	}
	for raw, want := range cases {
		if got := NormalizeFilter(raw); got != want {
			t.Errorf("NormalizeFilter(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestNormalizeFilter_StillFailsClosed pins the other half: normalizing
// SPELLING must not widen the set. An unrecognizable value must survive
// normalization unchanged so FilterValid rejects it — otherwise this helper
// would be a quiet "anything goes" and a garbage prop would render "all".
func TestNormalizeFilter_StillFailsClosed(t *testing.T) {
	for _, raw := range []string{"sort by date", "todos;drop table", "high priority", "open,done"} {
		got := NormalizeFilter(raw)
		if got != raw {
			t.Errorf("NormalizeFilter(%q) rewrote it to %q, want it untouched", raw, got)
		}
		if FilterValid(got) {
			t.Errorf("FilterValid(%q) accepted an unrecognizable filter", got)
		}
	}
}
