// Dashboard page signals.
package components

import "encoding/json"

// Signals seeds the dashboard page data-signals. Only the open-todo count
// is seeded here: the unread count is already owned by the shell
// ($inboxUnread, see internal/ui/shellSignals) so the inbox odometer
// reuses that signal instead of carrying a second copy of the same count.
type Signals struct {
	OpenTodos int `json:"openTodos"`
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
