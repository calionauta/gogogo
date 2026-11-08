// Todo listing slice (visibility mapping + identity).
//
// listVisible is the single read path every render and mutation shares:
// visibility-scoped store read, Go-side status/scope filters, and the
// per-row IsMine/CanDelete stamps (canDeleteRow). Identity helpers
// (roleOf/viewerOwner/roleOwner) and the display-signals builder live
// here too, so chi.go keeps routing, writes and fan-out.
package handlers

import (
	"context"
	"net/http"

	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/features/todo/service"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/rbac"
)

// roleOf resolves the request's session role ("admin" | "user" |
// "viewer" | ""). Delete gating branches on it: viewer never deletes,
// admin deletes any visible row, anyone deletes their own.
func (h *Handler) roleOf(r *http.Request) (owner string, role string) {
	sess, err := h.Auth.Session(r)
	if err != nil || sess == nil || sess.User == nil {
		return "", ""
	}
	return sess.User.ID, sess.Role
}

// canDeleteRow is the shared delete rule for HTTP + display: the owner
// always deletes their own rows (every role, viewer included — a viewer
// restriction applies to shared rows, never to your own private todos);
// otherwise viewer never, admin on any visible row. Callers have already
// proven the row visible to the requester (GetTodoVisible), so this
// only decides the destructive act — never the listing. Delegates to
// the domain service (single home).
func canDeleteRow(role, requester string, item todo.Todo) bool {
	return service.CanDeleteRow(role, requester, item)
}

// viewerOwner resolves the viewer flag for stream paths, which know the
// owner id but carry no session. A lookup miss fails open (affordance
// shown, policy still enforces) — display must never lock out, only
// enforcement denies.
func (h *Handler) viewerOwner(ctx context.Context, owner string) bool {
	if owner == "" || h.Q == nil {
		return false
	}
	u, err := h.Q.GetUser(ctx, owner)
	if err != nil {
		return false
	}
	return rbac.IsViewer(u.Role)
}

// toTodo maps a sqlc row onto the wire model. Delegates to the domain
// service (single home).
func toTodo(row dbgen.ExtTodoTodo) (todo.Todo, error) { return service.ToTodo(row) }

// toVisibleOne maps the single-row join (GetTodoVisible) the same way.
// Delegates to the domain service.
func toVisibleOne(row dbgen.GetTodoVisibleRow) (todo.Todo, error) {
	return service.ToVisibleOne(row)
}

// listVisible loads the rows one login may see — own rows plus every
// 'public' row — then applies the status filter ("all" | "active" |
// "completed") and the scope slice ("all" | "mine" | "shared") in Go.
// Delegates to the domain service (single home).
func (h *Handler) listVisible(ctx context.Context, owner, role, filter, scope string) ([]todo.Todo, error) {
	return h.svc().ListVisible(ctx, owner, role, filter, scope)
}

func filterOf(r *http.Request) string {
	if f := r.URL.Query().Get("filter"); f != "" {
		return f
	}
	return "all"
}

// scopeOf reads the listing slice ("all" | "mine" | "shared"). Unknown
// values read as "all" — a typo degrades to the full visible list,
// never to another tenant's private rows (the store scopes those out).
func scopeOf(r *http.Request) string {
	switch s := r.URL.Query().Get("scope"); s {
	case "mine", "shared":
		return s
	default:
		return "all"
	}
}

// displaySignals builds the list-display signals every render path shares:
// identity (viewer/admin/userID for per-row delete gating and authorship
// badges), scope + filter (tab state), and capability flags. Callers merge
// their ephemeral keys on top.
//
// SearchQuery is deliberately NOT derived here, even though a request may
// carry ?q=. $searchQuery is bound two-way to the search box, so the box owns
// it: any response that echoes the request's query can re-impose an OLDER
// value. A late response to ?q=kes landing after the user typed "kestrel"
// would set the box back to "kes" — the same clobber the handshake was fixed
// for, on the login page and the /search response instead. The box is seeded
// once per page load — the value flows through Signals.SearchQuery into the
// input's value=, and the input seeds $searchQuery from it (bind.ts, with
// {ifMissing: true}). That is the one place the URL's ?q= is authoritative;
// the search handler patches the list only. Use displaySignalsWithQuery for
// that one initial render.
func (h *Handler) displaySignals(r *http.Request, todos []todo.Todo, filter, scope string) todo.Signals {
	return h.displaySignalsWithQuery(r, todos, filter, scope, "")
}

// displaySignalsWithQuery is displaySignals plus an explicit SearchQuery, for
// the single caller whose render is the page load: seeding the box from the
// URL's ?q= is correct exactly once, before the client owns the signal. The
// field is `json:"-"`, so it is rendered into the input's value= rather than
// written into data-signals — the element then seeds $searchQuery itself.
func (h *Handler) displaySignalsWithQuery(
	r *http.Request, todos []todo.Todo, filter, scope, query string,
) todo.Signals {
	owner, role := h.roleOf(r)
	return todo.Signals{
		Todos: todos, Filter: filter, Scope: scope, ItemCount: len(todos),
		// Rendered into the search input's value= (see searchBox), NOT merged
		// into data-signals. The input owns $searchQuery via data-bind and seeds
		// it from value= on first render, so a later re-list cannot clobber what
		// the user typed. Writing it as a signal was the bug
		// (docs/realtime-recipe.md).
		SearchQuery:   query,
		SearchEnabled: h.searchEnabled(),
		// The mode + option list are SERVER signals (no `json:"-"`): the active
		// layer is the server's decision, and the picker's options must be what
		// boot actually built. `mode` here is the REQUESTED one; the handler
		// callers that render a list overwrite SearchMode with the resolved
		// active mode (activeSearchMode), so this default is only for callers
		// with no request to resolve (the initial page render seeds from the
		// default naturally).
		SearchMode:     h.activeSearchMode(""),
		SearchModes:    h.Search.Modes(),
		DagNatsEnabled: h.dagnatsUIEnabled(),
		IsViewer:       rbac.IsViewer(role),
		IsAdmin:        role == rbac.RoleAdmin,
		UserID:         owner,
		LLMEnabled:     h.LLM != nil && h.LLM.Configured(),
		SimulatedLLM:   h.SimulatedLLM != nil && h.SimulatedLLM.Configured(),
		// Recipes' row actions: configuration, so every render path carries the
		// same list and the row decides how to place it.
		RowActions: h.RowActions,
	}
}

// roleOwner resolves the stored role for stream paths, which know the owner
// id but carry no session. Lookup miss fails open (empty role: affordances
// shown, policy still enforces) — display must never lock out.
func (h *Handler) roleOwner(ctx context.Context, owner string) string {
	if owner == "" || h.Q == nil {
		return ""
	}
	u, err := h.Q.GetUser(ctx, owner)
	if err != nil {
		return ""
	}
	return u.Role
}
