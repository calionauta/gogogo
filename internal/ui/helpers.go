// SCOPE:core — Small render helpers for the shell.
//
// Templ cannot branch inside an attribute list or a short expression, so the
// values the shell computes rather than embeds live here: the count badge
// (capped), the stack section label, and the panel visibility seed.
package ui

import "strconv"

// maxBadgeCount caps a sidebar badge so a long list cannot stretch the rail.
const maxBadgeCount = 99

// formatCount renders a sidebar badge count, capped so a large list cannot
// stretch the rail.
func formatCount(n int) string {
	if n > maxBadgeCount {
		return "99+"
	}
	return strconv.Itoa(n)
}

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

// demoTitle explains what the toggle does, both ways.
func demoTitle(on bool) string {
	if on {
		return "Hide the machinery (product mode)"
	}
	return "Show what this page demonstrates (demo mode)"
}
