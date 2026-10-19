// SCOPE:feature — test-scope action declarations.
package store

import (
	"os"
	"testing"

	"github.com/calionauta/gogogo/features/genui/components"
)

// TestMain declares the actions these parsing tests exercise.
//
// Without a declaration the catalog refuses every model-supplied action (see
// checkDeclaredAction): the fixtures below emit `/addToCalendar` and
// `/setReminder`, so the registry has to contain them or the answer is
// rejected rather than rendered. That is the intended production behaviour —
// a build offers only what its features declared — and the fixtures have to
// opt in the same way a feature does.
func TestMain(m *testing.M) {
	if err := components.InstallActionRegistry([]components.ActionProvider{testActions{}}); err != nil {
		panic("chat store: test action registry install failed: " + err.Error())
	}
	os.Exit(m.Run())
}

// testActions is the minimal provider: two actions the fixtures already use,
// declared with the same shape a feature would.
type testActions struct{}

func (testActions) Actions() []components.ActionSpec {
	return []components.ActionSpec{
		{
			Label: "Add to Calendar", Method: "POST", URL: "/addToCalendar",
			Fields: map[string]string{}, When: "the user wants a plan added to a calendar",
		},
		{
			Label: "Set Reminder", Method: "POST", URL: "/setReminder",
			Fields: map[string]string{}, When: "the user wants to be reminded of a plan",
		},
		{
			Label: "Add Activity", Method: "POST", URL: "/add-activity",
			Fields: map[string]string{}, When: "the user wants an activity added to a plan",
		},
	}
}
