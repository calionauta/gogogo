// SCOPE:feature — signal wire-format contract for the todo feature.
package todo

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSignalsClientOwnedFieldsAreOmittedWhenEmpty pins the invariant behind a
// bug the browser smoke found: `newTitle` and `searchQuery` are bound two-way
// to text inputs (data-bind), so they are CLIENT-owned. The server's copy of
// such a value is always stale — the stored view only advances after a
// debounced search round-trips — so any background merge that carries them
// clobbers what the user is typing, and the input's next read (the debounced
// search) then fires with an empty value: the search silently does nothing.
//
// Both fields must be kept OFF the wire, and there are two correct tags for
// that. `json:"-"` excludes the field outright (searchQuery's shape: the input
// seeds the signal from its own value=, so the server never needs a copy).
// `omitempty` drops it only while zero, which is right for a field the server
// may legitimately set later (newTitle). What is NOT correct is a bare
// `json:"x"`: the zero value still marshals (`"searchQuery":""`), so even a
// supposedly "narrowing" merge wiped the box.
//
// This is the cheap, deterministic form of that guard: it fails at unit-test
// speed, without a browser, the moment someone writes a bare tag back.
func TestSignalsClientOwnedFieldsAreOmittedWhenEmpty(t *testing.T) {
	raw, err := json.Marshal(Signals{ItemCount: 3})
	if err != nil {
		t.Fatalf("marshal zero-value Signals: %v", err)
	}
	payload := string(raw)

	for _, field := range []string{"newTitle", "searchQuery"} {
		if strings.Contains(payload, `"`+field+`"`) {
			t.Errorf("Signals marshals %q for a zero value: the field is bound "+
				"to a data-bind input, so a background merge would overwrite "+
				"what the user typed. Keep it off the wire — `json:\"-\"` (the "+
				"client seeds it from value=) or `omitempty` (the server may set "+
				"it later).\npayload: %s",
				field, payload)
		}
	}

	// The non-client-owned fields must still be present: a merge that dropped
	// itemCount would leave the count badge stale.
	if !strings.Contains(payload, `"itemCount":3`) {
		t.Errorf("Signals stopped emitting itemCount, which every re-list "+
			"relies on:\npayload: %s", payload)
	}
}
