// SCOPE:core — Small render helpers for the shell.
//
// Templ cannot branch inside an attribute list or a short expression, so the
// values the shell computes rather than embeds live here.
package ui

import "github.com/calionauta/gogogo/internal/rbac"

// panelVisibilitySignals seeds the client-side visibility signal for the
// state panel. The server decides the initial value from the demo cookie;
// all later flips are signal-only (toggle/close persist via fetch, no
// navigation, no flash).
func panelVisibilitySignals(demo bool) string {
	if demo {
		return `{"panelVisible":true}`
	}
	return `{"panelVisible":false}`
}

// activeCurrent returns aria-current's value for a nav item, or "" when it is
// not the current one: templ cannot branch inside an attribute list, so the
// value is computed here instead.
func activeCurrent(active bool) string {
	if active {
		return "page"
	}
	return ""
}

// DemoHref is the toggle's target: the same page, with demo mode flipped. The
// mode is a session cookie set by /_ui/demo, so this stays a plain link — no
// script, and the state survives every module until the browser session ends.
func (d Data) DemoHref() string {
	next := "/"
	if m, ok := ByID(d.Module); ok {
		next = m.URL
	}
	on := "1"
	if d.Demo {
		on = "0"
	}
	return "/_ui/demo?on=" + on + "&next=" + next
}

// ariaPressed spells the toggle's two states for assistive tech.
func ariaPressed(on bool) string {
	if on {
		return "true"
	}
	return "false"
}

// HasDemo reports whether this page has anything to show in the "How it works"
// panel: the module's own machinery, the stack key, or both. It is the ONE
// condition the toggle and the panel share — a page that declares a Stack but
// no Panel (notes, room, whiteboard: their demo IS the page) still gets the
// toggle, because the panel then carries the stack key instead of hiding the
// page's own evidence behind a button.
func (d Data) HasDemo() bool {
	return !rbac.IsViewer(d.Role) && (d.Panel != nil || len(d.Stack) > 0)
}

// demoTitle explains what the toggle does, both ways.
func demoTitle(on bool) string {
	if on {
		return "Hide the machinery (product mode)"
	}
	return "Show what this page demonstrates (demo mode)"
}
