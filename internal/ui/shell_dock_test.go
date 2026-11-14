// AppDock role contract: the mobile bottom bar must not offer
// a destination its policy denies.
package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestAppDockMenuIsTheAccordionToggle pins the drawer toggle's shape: the
// chevron sits left of the word (a daisyUI dock child is a column, so the row
// is a project rule), and the button publishes the state the rotation is keyed
// off instead of rotating itself.
func TestAppDockMenuIsTheAccordionToggle(t *testing.T) {
	var buf bytes.Buffer
	if err := AppDock("todo", "user").Render(context.Background(), &buf); err != nil {
		t.Fatalf("render dock: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, `class="app-dock-menu"`) {
		t.Fatal("the Menu button carries no class; the dock's column rule wins and the icon stacks above the word")
	}
	if !strings.Contains(html, "data-attr:aria-expanded=") {
		t.Error("the toggle publishes no expanded state, so the chevron cannot rotate with the drawer")
	}
	// The glyph is inline SVG, so the class is what names it: the rotation rule
	// in chrome.css is keyed off `app-dock-chevron`.
	if !strings.Contains(html, "app-dock-chevron") {
		t.Error("the toggle is not the accordion chevron (no app-dock-chevron class)")
	}
	menu := strings.Index(html, "app-dock-menu")
	chevron := strings.Index(html[menu:], "app-dock-chevron")
	label := strings.Index(html[menu:], `>Menu<`)
	if chevron < 0 || label < 0 {
		t.Fatal("the Menu button lost its chevron or its label")
	}
	if chevron > label {
		t.Error("the chevron comes after the word in the markup; the row would read Menu then the icon")
	}
}

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
