// The rail's unread surfaces: one count when expanded, one dot when collapsed.
package ui

import (
	"context"
	"strings"
	"testing"
)

// openingTag returns the start tag that carries id, so an assertion can pin
// the attributes of one node without matching a neighbour's classes.
func openingTag(t *testing.T, html, id string) string {
	t.Helper()
	i := strings.Index(html, id)
	if i < 0 {
		t.Fatalf("no %s in the rendered shell", id)
	}
	end := strings.Index(html[i:], ">")
	if end < 0 {
		t.Fatalf("unterminated start tag for %s", id)
	}
	return html[i : i+end]
}

// TestRailInboxCountIsAnIndicator pins the expanded rail: the count is a
// daisyUI indicator item hanging off the row label, and it disappears (via the
// pattern's own is-drawer-close variant) when the rail collapses — the label it
// is anchored to is gone in a 4.5rem column.
func TestRailInboxCountIsAnIndicator(t *testing.T) {
	html := renderShell(WithInboxUnread(context.Background(), 7), t)
	tag := openingTag(t, html, `id="rail-inbox-badge"`)
	for _, want := range []string{
		"indicator-item",
		"badge badge-primary badge-xs",
		"is-drawer-close:hidden",
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("expanded rail count missing %q: %s", want, tag)
		}
	}
}

// TestRailInboxDotCarriesTheCollapsedState pins the other half: a bare dot on
// the icon corner, visible only while the rail is closed, with the number kept
// for screen readers. Without it the collapsed rail loses the unread fact
// entirely (the count is hidden with the label).
func TestRailInboxDotCarriesTheCollapsedState(t *testing.T) {
	html := renderShell(WithInboxUnread(context.Background(), 7), t)
	tag := openingTag(t, html, `id="rail-inbox-dot"`)
	for _, want := range []string{
		"indicator-item status status-primary",
		"is-drawer-open:hidden",
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("collapsed rail marker missing %q: %s", want, tag)
		}
	}
	if !strings.Contains(html, "7 unread") {
		t.Error("the collapsed marker dropped the screen-reader count")
	}
}

// TestRailInboxBadgeKeepsItsTextForTheSeed pins the session.js contract: the
// BroadcastChannel seeds itself from this node's text, so the element id must
// stay on the text-bearing count (not on the wrapper or the dot), even while
// the collapsed rail hides it.
func TestRailInboxBadgeKeepsItsTextForTheSeed(t *testing.T) {
	html := renderShell(WithInboxUnread(context.Background(), 12), t)
	i := strings.Index(html, `id="rail-inbox-badge"`)
	if i < 0 {
		t.Fatal("no #rail-inbox-badge")
	}
	end := strings.Index(html[i:], "</span>")
	if end < 0 {
		t.Fatal("unterminated #rail-inbox-badge")
	}
	if node := html[i : i+end]; !strings.Contains(node, ">12") {
		t.Fatalf("#rail-inbox-badge no longer carries the count text: %s", node)
	}
}

// TestRailItemKeepsItsNameWhenCollapsed pins the screen-reader name of a
// collapsed row: the label is display:none there, so without a screen-reader
// copy the link is announced by its count alone ("4 unread") and the
// destination is lost. The copy shows only in the collapsed width, so the
// expanded row is not read twice.
func TestRailItemKeepsItsNameWhenCollapsed(t *testing.T) {
	html := renderShell(WithInboxUnread(context.Background(), 4), t)
	for _, want := range []string{
		`<span class="sr-only is-drawer-open:hidden">Inbox</span>`,
		`<span class="sr-only is-drawer-open:hidden">Todo</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("collapsed rail row missing its screen-reader name %q", want)
		}
	}
}

// TestRailInboxMarkerHidesAtZero pins the zero state for both nodes: the
// server renders them hidden (not absent), because the live patches need a
// node to turn back on.
func TestRailInboxMarkerHidesAtZero(t *testing.T) {
	html := renderShell(context.Background(), t)
	for _, id := range []string{`id="rail-inbox-badge"`, `id="rail-inbox-dot"`} {
		if tag := openingTag(t, html, id); !strings.Contains(tag, "hidden") {
			t.Errorf("%s is not hidden at zero: %s", id, tag)
		}
	}
}
