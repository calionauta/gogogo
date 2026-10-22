// SCOPE:core — The one shell's module registry.
//
// Every destination in the app is a Module value. Adding a 9th surface is one
// entry here, not a layout change (that is criterion C-3 of the revamp brief:
// tmp/gogogo-ui-revamp/interfaces/selected-interface.md). The shell renders
// whatever this slice returns, grouped and filtered by role, so the sidebar can
// never disagree with the routes the mux actually serves.
//
// Order is deliberate and user-visible: collab surfaces first (the common
// paths), then the LLM demos, then the operator surfaces.
package ui

import "github.com/calionauta/gogogo/internal/rbac"

// Module is one destination in the shell.
type Module struct {
	ID    string // route suffix + active marker, e.g. "todo"
	Title string // sidebar label and module header title
	Group string // GroupCollab | GroupAI | GroupOperate
	Icon  string // iconify name (material-symbols)
	URL   string // link target
	Admin bool   // only visible to admins; the route stays RBAC-enforced
	Count int    // optional right-aligned badge (0 renders nothing)
}

// Group labels, in render order.
const (
	GroupCollab  = "Collab"
	GroupAI      = "AI"
	GroupOperate = "Operate"
)

// Modules is the single source for the shell's navigation. It mirrors
// internal/server's route mounts; a module whose route is trimmed out of a
// build is expected to be trimmed here too (the capability registry owns that
// decision, not this list).
var Modules = []Module{
	{ID: "todo", Title: "Todo", Group: GroupCollab, Icon: "material-symbols:checklist", URL: "/todo"},
	{ID: "whiteboard", Title: "Whiteboard", Group: GroupCollab, Icon: "material-symbols:draw", URL: "/whiteboard"},
	{ID: "notes", Title: "Notes", Group: GroupCollab, Icon: "material-symbols:description", URL: "/notes"},
	{ID: "genui", Title: "Ask AI", Group: GroupAI, Icon: "material-symbols:auto-awesome", URL: "/genui"},
	{ID: "chat", Title: "Chat", Group: GroupAI, Icon: "material-symbols:forum", URL: "/chat"},
	{ID: "review", Title: "Review", Group: GroupAI, Icon: "material-symbols:fact-check", URL: "/review"},
	{ID: "room", Title: "Room", Group: GroupOperate, Icon: "material-symbols:groups", URL: "/room"},
	{ID: "config", Title: "Config", Group: GroupOperate, Icon: "material-symbols:settings", URL: "/config", Admin: true},
}

// Visible returns the modules the given role may see. An empty role (guest)
// sees the non-admin surfaces, matching the navbar's previous behaviour; the
// route itself is still enforced by the RBAC policy.
func Visible(role string) []Module {
	out := make([]Module, 0, len(Modules))
	for _, m := range Modules {
		if m.Admin && !rbac.IsAdmin(role) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// ModuleGroup is one labelled block of the sidebar.
type ModuleGroup struct {
	Label   string
	Modules []Module
}

// Grouped returns the visible modules grouped in render order, dropping empty
// groups (so a role that only sees Collab renders one heading, not three).
func Grouped(role string) []ModuleGroup {
	visible := Visible(role)
	var out []ModuleGroup
	for _, label := range []string{GroupCollab, GroupAI, GroupOperate} {
		var group []Module
		for _, m := range visible {
			if m.Group == label {
				group = append(group, m)
			}
		}
		if len(group) > 0 {
			out = append(out, ModuleGroup{Label: label, Modules: group})
		}
	}
	return out
}

// ByID returns the module with the given id. ok is false when a page renders
// under an id that is not a destination (the landing page, the login page:
// both are Decide surfaces outside the shell, so they never call ByID).
func ByID(id string) (Module, bool) {
	for _, m := range Modules {
		if m.ID == id {
			return m, true
		}
	}
	return Module{}, false
}
