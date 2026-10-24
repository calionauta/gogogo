// SCOPE:core — AppDock role contract: the mobile bottom bar must not offer
// a destination its policy denies.
package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestAppDockViewerSwapsChatForNotes pins the viewer dock: the viewer policy
// denies /chat, so the third destination is Notes (viewer-allowed), not Chat.
// Plain users and admins keep Todo/Board/Chat.
func TestAppDockViewerSwapsChatForNotes(t *testing.T) {
	render := func(role string) string {
		t.Helper()
		var buf bytes.Buffer
		if err := AppDock("todo", role).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render dock: %v", err)
		}
		return buf.String()
	}

	viewer := render("viewer")
	for _, want := range []string{`href="/todo"`, `href="/whiteboard"`, `href="/notes"`} {
		if !strings.Contains(viewer, want) {
			t.Errorf("viewer dock missing %q (viewer policy allows it)", want)
		}
	}
	if strings.Contains(viewer, `href="/chat"`) {
		t.Error("viewer dock links /chat (viewer policy denies it: tap 403s)")
	}

	for _, role := range []string{"user", "admin"} {
		got := render(role)
		for _, want := range []string{`href="/todo"`, `href="/whiteboard"`, `href="/chat"`} {
			if !strings.Contains(got, want) {
				t.Errorf("%s dock missing %q", role, want)
			}
		}
		if strings.Contains(got, `href="/notes"`) {
			t.Errorf("%s dock links /notes (third slot is Chat here)", role)
		}
	}
}
