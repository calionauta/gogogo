// The kiosk page's Datastar seed.
package components

import "encoding/json"

// Seeds is the page's initial signal set. A map rather than a struct on purpose:
// the view writes the keys next to the markup that reads them, and there is no
// second place to keep a field name in step.
type Seeds map[string]any

// MarshalSignals renders the seed for data-signals. A marshal failure is
// impossible for this value (strings and bools) and renders an empty object
// rather than breaking the page: a signal that does not exist reads as empty,
// which is the same state the seed carries.
func MarshalSignals(s Seeds) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "{}"
	}
	return string(b)
}
