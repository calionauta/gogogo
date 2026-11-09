// Dashboard page signals.
package components

import "encoding/json"

// Signals seeds the dashboard page data-signals for the metric odometers.
// $inboxUnread stays shell-owned (see internal/ui/shellSignals) and is not
// duplicated here.
type Signals struct {
	OpenTodos   int `json:"openTodos"`
	DoneTodos   int `json:"doneTodos"`
	TotalTodos  int `json:"totalTodos"`
	SharedTodos int `json:"sharedTodos"`
	TotalInbox  int `json:"totalInbox"`
	ChatTotal   int `json:"chatTotal"`
	ChatHuman   int `json:"chatHuman"`
	NotesCount  int `json:"notesCount"`
	BoardsCount int `json:"boardsCount"`
	CreditsBal  int `json:"creditsBal"`
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
