// SCOPE:feature — The todo state-panel gate: viewers get no machinery dock.
//
// TodoStatePanel is the queue+retry demo, the AI suggest affordance and the
// durable workflow. Every one of them POSTs to an endpoint the viewer policy
// denies (retry-demo, suggest, onboarding/start — pinned deny in
// internal/rbac/rbac_test.go TestRBACViewerPolicy), so rendering the panel
// for a viewer would offer buttons that 403. The shell skips a nil Panel
// (no aside, no Demo toggle), so viewers get a clean page instead of an
// empty dock.
package components

import (
	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/features/todo"
)

// statePanelFor returns the machinery panel for roles that may run it, and
// nil for viewers. Nil flows into ui.Data.Panel, which the shell treats as
// "no machinery" (same as the panel-less modules).
func statePanelFor(signals todo.Signals) templ.Component {
	if signals.IsViewer {
		return nil
	}
	return TodoStatePanel(signals)
}
