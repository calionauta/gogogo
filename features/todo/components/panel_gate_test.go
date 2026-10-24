// SCOPE:feature — Viewer machinery-panel contract (red-proof).
//
// TodoStatePanel is queue+retry, AI suggest and durable workflow — every
// button in it POSTs to an endpoint the viewer policy denies (retry-demo,
// suggest, onboarding/start). Rendering it for a viewer would offer buttons
// that 403, so the layout passes a nil panel and the shell docks nothing.
package components

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
)

// TestStatePanelForNilForViewers pins the gate: viewers get no panel (the
// shell skips a nil Panel — no aside, no Demo toggle), everyone else gets
// the machinery dock.
func TestStatePanelForNilForViewers(t *testing.T) {
	if got := statePanelFor(todo.Signals{IsViewer: true}); got != nil {
		t.Fatal("viewer must get a nil state panel")
	}
	if got := statePanelFor(todo.Signals{}); got == nil {
		t.Fatal("plain user must get the machinery panel")
	}
}

// TestLayoutHidesMachineryFromViewers renders the whole /todo page: a viewer
// must see no state-panel aside and none of the denied POST affordances,
// while a plain user keeps the full machinery dock (which also proves the
// test renders the panel at all — no vacuous pass).
func TestLayoutHidesMachineryFromViewers(t *testing.T) {
	render := func(signals todo.Signals, email, role string) string {
		t.Helper()
		var buf bytes.Buffer
		err := Layout("Todos — test", signals, email, role, "test", "abc1234", true, true).Render(
			context.Background(), &buf)
		if err != nil {
			t.Fatalf("render layout for %s: %v", role, err)
		}
		return buf.String()
	}

	viewer := render(todo.Signals{IsViewer: true, DagNatsEnabled: true}, "v@example.com", "viewer")
	for _, banned := range []string{
		`id="state-panel"`,
		`retry-demo`,
		`/api/onboarding/start`,
		`Run durable workflow`,
		`Demo mode and use`,
	} {
		if strings.Contains(viewer, banned) {
			t.Errorf("viewer page must not offer %q (policy denies it)", banned)
		}
	}

	user := render(todo.Signals{DagNatsEnabled: true}, "u@example.com", "user")
	for _, want := range []string{
		`id="state-panel"`,
		`retry-demo`,
		`/api/onboarding/start`,
	} {
		if !strings.Contains(user, want) {
			t.Errorf("user page must keep the machinery dock (%q missing)", want)
		}
	}
}
