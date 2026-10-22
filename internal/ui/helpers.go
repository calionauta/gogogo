// SCOPE:core — Small render helpers for the shell.
//
// Templ cannot branch inside an attribute list or a short expression, so the
// two values the shell computes rather than embeds live here: the count badge
// (capped) and the stack key's summary line.
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

// stackSummary is the collapsed stack key's label: "stack 7".
func stackSummary(n int) string {
	return "stack " + strconv.Itoa(n)
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
