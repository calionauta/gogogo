// SCOPE:core — Small render helpers for the shell.
//
// Templ cannot branch inside an attribute list or a short expression, so the
// values the shell computes rather than embeds live here.
package ui

import (
	"strconv"

	"github.com/calionauta/gogogo/internal/rbac"
)

// panelVisibilitySignals seeds the client-side visibility signal for the
// demo panel. The server decides the initial value from the demo cookie;
// all later flips are signal-only (toggle/close persist via fetch, no
// navigation, no flash).
//
// panelMode names which section the panel shows. The two panel buttons each
// open their own section, so the panel never stacks machinery over the stack
// key: one request, one answer. A page that only has one section seeds that
// one; a page with both seeds State.
func panelVisibilitySignals(demo bool, hasState, hasStack bool) string {
	mode := panelModeName(hasState, hasStack)
	return `{"panelVisible":` + boolLiteral(demo && (hasState || hasStack)) +
		`,"panelMode":"` + mode + `"}`
}

// panelModeName is the server's default section: State when the page has
// machinery, Stack otherwise. It is the initial value only — the buttons own
// every later write.
func panelModeName(hasState, hasStack bool) string {
	if hasState || !hasStack {
		return "state"
	}
	return "stack"
}

// boolLiteral spells a bool for the inline signal JSON.
func boolLiteral(b bool) string {
	if b {
		return "true"
	}
	return "false"
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

// HasState reports whether this page has machinery to show in the State panel:
// a queue, a model call, phases that run beside the page. The State button and
// the State section share this one condition.
func (d Data) HasState() bool {
	return !rbac.IsViewer(d.Role) && d.Panel != nil
}

// HasStack reports whether this page has a stack key for the Stack panel. A
// page that declares a Stack but no Panel (notes, room, whiteboard: their demo
// IS the page) still gets its own button, because the key is evidence the page
// owns rather than machinery it does not.
func (d Data) HasStack() bool {
	return !rbac.IsViewer(d.Role) && len(d.Stack) > 0
}

// HasDemo reports whether this page has anything to show in the demo panel.
// Kept as the shared condition for anything that asks the broader question.
func (d Data) HasDemo() bool {
	return d.HasState() || d.HasStack()
}

// stateTitle and stackTitle explain what each button toggles. Static on
// purpose: the server cannot know which section is open after the client
// switches modes, and a title that claims "hide" for a section that is not
// showing is worse than one that says how to toggle.
func stateTitle() string {
	return "Show or hide the running machinery (queue, model calls, phases)"
}

func stackTitle() string {
	return "Show or hide the stack key"
}

// shellSignals seeds the two signals the shell owns on every page: the mobile
// drawer and the rail's unread count. The count is rendered server-side (the
// page middleware) and then kept current by the leader session stream
// (session.js → gogogo:unread), so the badge never needs a second connection.
func shellSignals(unread int) string {
	return `{"railOpen":false,"inboxUnread":` + strconv.Itoa(unread) + `}`
}
