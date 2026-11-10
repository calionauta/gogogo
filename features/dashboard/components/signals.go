package components

import "encoding/json"

// Signals is the dashboard page's own signal set: one entry per metric the
// page shows, keyed by that metric's signal name.
//
// It is a map, not a struct, on purpose. The metric set is generic — every
// installed recipe declares its own, including recipes this build has never
// seen — so a fixed struct would need a field per recipe and would silently
// drop the signal of any recipe that is not on the list. The odometer then
// animates to a signal nobody seeded, which is a dead counter, not an
// error. The map carries exactly what the page shows.
//
// The key is a Datastar signal name, so it must be a JavaScript
// identifier: recipe.MetricProblems rejects a malformed one at boot.
type Signals map[string]int

// MarshalSignals renders Signals for data-signals. Never fails the page:
// unmarshalable input cannot happen from the map above, so failure
// falls back to an empty object.
func MarshalSignals(s Signals) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}
