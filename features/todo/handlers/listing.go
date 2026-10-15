// SCOPE:feature — todo listing slice (visibility mapping + identity).
//
// listVisible is the single read path every render and mutation shares:
// visibility-scoped store read, Go-side status/scope filters, and the
// per-row IsMine/CanDelete stamps (canDeleteRow). Identity helpers
// (roleOf/viewerOwner/roleOwner) and the display-signals builder live
// here too, so chi.go keeps routing, writes and fan-out.
package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
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
// only decides the destructive act — never the listing.
func canDeleteRow(role, requester string, item todo.Todo) bool {
	if requester != "" && requester == item.Owner {
		return true
	}
	if rbac.IsViewer(role) {
		return false
	}
	return role == rbac.RoleAdmin
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

// toTodo maps a sqlc row onto the wire model. Timestamps are caller-written
// RFC3339 (dbschema.TimeFormat); a row that does not parse is data corruption,
// flagged as a 500 rather than rendered half-empty.
func toTodo(row dbgen.Todo) (todo.Todo, error) {
	created, err := time.Parse(dbschema.TimeFormat, row.Created)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo %s created %q: %w", row.ID, row.Created, err)
	}
	updated, err := time.Parse(dbschema.TimeFormat, row.Updated)
	if err != nil {
		return todo.Todo{}, fmt.Errorf("todo %s updated %q: %w", row.ID, row.Updated, err)
	}
	return todo.Todo{
		ID:         row.ID,
		Title:      row.Title,
		Completed:  row.Completed,
		CreatedAt:  created,
		UpdatedAt:  updated,
		Owner:      row.Owner,
		Visibility: row.Visibility,
	}, nil
}

// toVisibleTodo maps a visibility-scoped join row (todo + author email)
// onto the wire model. Same corruption contract as toTodo.
func toVisibleTodo(row dbgen.ListVisibleTodosRow) (todo.Todo, error) {
	item, err := toTodo(dbgen.Todo{
		ID: row.ID, Owner: row.Owner, Title: row.Title,
		Completed: row.Completed, IdemKey: row.IdemKey,
		Visibility: row.Visibility, Created: row.Created, Updated: row.Updated,
	})
	if err != nil {
		return todo.Todo{}, err
	}
	item.OwnerEmail = row.OwnerEmail
	return item, nil
}

// toVisibleOne maps the single-row join (GetTodoVisible) the same way.
func toVisibleOne(row dbgen.GetTodoVisibleRow) (todo.Todo, error) {
	item, err := toTodo(dbgen.Todo{
		ID: row.ID, Owner: row.Owner, Title: row.Title,
		Completed: row.Completed, IdemKey: row.IdemKey,
		Visibility: row.Visibility, Created: row.Created, Updated: row.Updated,
	})
	if err != nil {
		return todo.Todo{}, err
	}
	item.OwnerEmail = row.OwnerEmail
	return item, nil
}

// listVisible loads the rows one login may see — own rows plus every
// 'public' row — then applies the status filter ("all" | "active" |
// "completed") and the scope slice ("all" | "mine" | "shared") in Go.
// Unknown values read as "all". Private rows of other users never leave
// the store: the WHERE clause scopes before Go ever sees them.
//
// role is the requester's session role: each returned row is stamped with
// IsMine/CanDelete, so templates render affordances without re-deriving
// the rule (single home: canDeleteRow).
func (h *Handler) listVisible(ctx context.Context, owner, role, filter, scope string) ([]todo.Todo, error) {
	rows, err := h.Q.ListVisibleTodos(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]todo.Todo, 0, len(rows))
	for _, row := range rows {
		item, err := toVisibleTodo(row)
		if err != nil {
			return nil, err
		}
		if owner != "" {
			switch scope {
			case "mine":
				if item.Owner != owner {
					continue
				}
			case "shared":
				if item.Owner == owner || !item.IsShared() {
					continue
				}
			}
		}
		switch filter {
		case "active":
			if item.Completed {
				continue
			}
		case "completed":
			if !item.Completed {
				continue
			}
		}
		item.IsMine = owner != "" && item.Owner == owner
		item.CanDelete = canDeleteRow(role, owner, item)
		out = append(out, item)
	}
	return out, nil
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
func (h *Handler) displaySignals(r *http.Request, todos []todo.Todo, filter, scope string) todo.Signals {
	owner, role := h.roleOf(r)
	return todo.Signals{
		Todos: todos, Filter: filter, Scope: scope, ItemCount: len(todos),
		DagNatsEnabled: h.dagnatsUIEnabled(),
		IsViewer:       rbac.IsViewer(role),
		IsAdmin:        role == rbac.RoleAdmin,
		UserID:         owner,
		LLMEnabled:     h.LLM != nil && h.LLM.Configured(),
		SimulatedLLM:   h.SimulatedLLM != nil && h.SimulatedLLM.Configured(),
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
