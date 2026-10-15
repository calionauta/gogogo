// SCOPE:feature — Shared (public) vs private todo contract tests (red-proof).
//
// Demo1 and demo2 must prove live collab from two browsers: a 'public' todo
// created by one login is listed, toggled and re-rendered by the other,
// while 'private' rows stay invisible across tenants. Visibility is listing
// scope only — what a role may DO stays in the handler (owner always
// deletes own, viewer included; viewer never deletes shared; admin
// deletes any visible row).
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/queue"
)

// TestHandlersSharedTodoCrossUser pins the collab demo: A's public row shows
// up in B's fragment (with the Shared badge + author), A's private row does
// not, and the scope slice filters both sides.
func TestHandlersSharedTodoCrossUser(t *testing.T) {
	f := newHandlersFixture(t)
	a := f.signup("sharer@example.com")
	b := f.signup("seer@example.com")

	if rec := f.do(http.MethodPost, "/api/todos", "title=Shared+task&visibility=public", a); rec.Code != http.StatusOK {
		t.Fatalf("share create: status = %d (%s)", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodPost, "/api/todos", "title=Private+task", a); rec.Code != http.StatusOK {
		t.Fatalf("private create: status = %d (%s)", rec.Code, rec.Body.String())
	}

	frag := f.do(http.MethodGet, "/api/todos/fragment?filter=all&scope=all", "", b)
	body := frag.Body.String()
	if !strings.Contains(body, "Shared task") {
		t.Error("B's fragment must list A's shared todo")
	}
	if !strings.Contains(body, "Shared</span>") {
		t.Error("shared row must carry the Shared badge")
	}
	if !strings.Contains(body, "sharer@example.com") {
		t.Error("shared row must name its author")
	}
	if strings.Contains(body, "Private task") {
		t.Error("B's fragment leaks A's private todo")
	}

	mine := f.do(http.MethodGet, "/api/todos/fragment?filter=all&scope=mine", "", b)
	if strings.Contains(mine.Body.String(), "Shared task") {
		t.Error("scope=mine must not list another user's shared todo")
	}
	shared := f.do(http.MethodGet, "/api/todos/fragment?filter=all&scope=shared", "", b)
	if !strings.Contains(shared.Body.String(), "Shared task") {
		t.Error("scope=shared must list A's shared todo for B")
	}
	// A sees both rows under scope=all, only their own under scope=mine.
	aAll := f.do(http.MethodGet, "/api/todos/fragment?filter=all&scope=all", "", a)
	if !strings.Contains(aAll.Body.String(), "Shared task") || !strings.Contains(aAll.Body.String(), "Private task") {
		t.Error("A's scope=all must list both own rows")
	}
}

// TestHandlersSharedTodoToggleDelete pins the permission half: any login
// toggles a shared row; only the author (or an admin) deletes it.
func TestHandlersSharedTodoToggleDelete(t *testing.T) {
	f := newHandlersFixture(t)
	a := f.signup("owner-x@example.com")
	b := f.signup("peer-x@example.com")
	ctx := context.Background()

	f.do(http.MethodPost, "/api/todos", "title=Shared+row&visibility=public", a)
	ownerA := f.ownerOf(t, a)
	rows, err := f.q.ListTodosByOwner(ctx, ownerA)
	if err != nil || len(rows) != 1 {
		t.Fatalf("setup list = %d, %v", len(rows), err)
	}
	id := rows[0].ID

	// B toggles A's shared row: allowed.
	if rec := f.do(http.MethodPost, "/api/todos/"+id+"/toggle", "", b); rec.Code != http.StatusOK {
		t.Fatalf("peer toggle shared: status = %d, want 200", rec.Code)
	}
	got, err := f.q.GetTodo(ctx, dbgen.GetTodoParams{ID: id, Owner: ownerA})
	if err != nil || !got.Completed {
		t.Fatalf("after peer toggle completed = %+v, %v; want true", got, err)
	}

	// B deletes A's shared row: visible (no 404) but forbidden.
	if rec := f.do(http.MethodPost, "/api/todos/"+id+"/delete", "", b); rec.Code != http.StatusForbidden {
		t.Errorf("peer delete shared: status = %d, want 403", rec.Code)
	}
	if n, _ := f.q.CountTodosByOwner(ctx, ownerA); n != 1 {
		t.Fatalf("forbidden delete removed the row (count = %d)", n)
	}

	// Private rows of others stay 404 on every verb (no existence leak).
	f.do(http.MethodPost, "/api/todos", "title=Secret+x", a)
	priv, _ := f.q.ListTodosByOwner(ctx, ownerA)
	var privID string
	for _, r := range priv {
		if r.Title == "Secret x" {
			privID = r.ID
		}
	}
	if rec := f.do(http.MethodPost, "/api/todos/"+privID+"/toggle", "", b); rec.Code != http.StatusNotFound {
		t.Errorf("peer toggle private: status = %d, want 404", rec.Code)
	}
	if rec := f.do(http.MethodPost, "/api/todos/"+privID+"/delete", "", b); rec.Code != http.StatusNotFound {
		t.Errorf("peer delete private: status = %d, want 404", rec.Code)
	}

	// A deletes their own shared row: allowed.
	if rec := f.do(http.MethodPost, "/api/todos/"+id+"/delete", "", a); rec.Code != http.StatusOK {
		t.Errorf("owner delete shared: status = %d, want 200", rec.Code)
	}
}

// TestHandlersSharedTodoViewerAndAdmin pins the role edges: a viewer cannot
// delete even their own row; an admin deletes another login's shared row.
func TestHandlersSharedTodoViewerAndAdmin(t *testing.T) {
	f := newHandlersFixture(t)
	ctx := context.Background()

	ownerC := f.signup("owner-y@example.com")
	f.do(http.MethodPost, "/api/todos", "title=Team+board&visibility=public", ownerC)
	ownerID := f.ownerOf(t, ownerC)
	rows, _ := f.q.ListTodosByOwner(ctx, ownerID)
	if len(rows) != 1 {
		t.Fatalf("setup rows = %d, want 1", len(rows))
	}
	id := rows[0].ID

	viewerC := f.signup("viewer-y@example.com")
	if err := f.q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "viewer", Updated: dbschema.Now(), Email: "viewer-y@example.com",
	}); err != nil {
		t.Fatalf("set viewer role: %v", err)
	}
	// Viewer creates their own row, then deletes it: owners always delete
	// their own rows, viewer included (restrictions apply to shared rows).
	f.do(http.MethodPost, "/api/todos", "title=Viewer+own", viewerC)
	viewerID := f.ownerOf(t, viewerC)
	vrows, _ := f.q.ListTodosByOwner(ctx, viewerID)
	if len(vrows) != 1 {
		t.Fatalf("viewer setup rows = %d, want 1", len(vrows))
	}
	if rec := f.do(http.MethodPost, "/api/todos/"+vrows[0].ID+"/delete", "", viewerC); rec.Code != http.StatusOK {
		t.Errorf("viewer delete own: status = %d, want 200", rec.Code)
	}
	if n, _ := f.q.CountTodosByOwner(ctx, viewerID); n != 0 {
		t.Errorf("after viewer delete own count = %d, want 0", n)
	}

	adminC := f.signup("admin-y@example.com")
	if err := f.q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "admin", Updated: dbschema.Now(), Email: "admin-y@example.com",
	}); err != nil {
		t.Fatalf("set admin role: %v", err)
	}
	if rec := f.do(http.MethodPost, "/api/todos/"+id+"/delete", "", adminC); rec.Code != http.StatusOK {
		t.Errorf("admin delete shared: status = %d, want 200", rec.Code)
	}
	if n, _ := f.q.CountTodosByOwner(ctx, ownerID); n != 0 {
		t.Errorf("after admin delete count = %d, want 0", n)
	}
}

// TestHandlersRecordRelistPreservesTabSlice pins L1: a record-event
// re-list renders the tab's stored listing slice, not all/all. B sits on
// scope=mine; when A's shared row mutates, B's re-listed fragment keeps
// only B's own rows instead of yanking the tab back to the full list.
func TestHandlersRecordRelistPreservesTabSlice(t *testing.T) {
	f := newHandlersFixture(t)
	a := f.signup("relist-a@example.com")
	b := f.signup("relist-b@example.com")

	f.do(http.MethodPost, "/api/todos", "title=Shared+board&visibility=public", a)
	f.do(http.MethodPost, "/api/todos", "title=Mine+only", b)

	// B opens scope=mine through the real endpoint (stores the view pref
	// against B's stream clientID, like the scope tab click does).
	f.do(http.MethodGet, "/api/todos?filter=all&scope=mine&clientID=tab-b", "", b)
	if got := f.h.Hub.ViewOf("tab-b"); got != (queue.ViewPref{Filter: "all", Scope: "mine"}) {
		t.Fatalf("stored view = %+v, want all/mine", got)
	}

	rec := httptest.NewRecorder()
	sse := sseGen(t, rec)
	ownerB := f.ownerOf(t, b)
	if err := f.h.applyRecordEvent(context.Background(), sse, ownerB, "tab-b", true,
		recordEvent{Event: "toggled", ID: "x"}); err != nil {
		t.Fatalf("applyRecordEvent: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Mine only") {
		t.Error("mine-scoped re-list must keep B's own row")
	}
	if strings.Contains(body, "Shared board") {
		t.Error("mine-scoped re-list must not pull A's shared row back in")
	}
}

// TestHandlersMutationsPreserveTabSlice pins the URL contract every
// list-affecting mutation must honor: toggle/clear/delete re-list with
// the request's filter AND scope, so acting from Mine/Shared never
// yanks the tab back to All. Red-proof: drop &scope= from any mutation
// URL and the first assertion fails (shared row leaks back in).
func TestHandlersMutationsPreserveTabSlice(t *testing.T) {
	f := newHandlersFixture(t)
	a := f.signup("slice-a@example.com")
	b := f.signup("slice-b@example.com")
	ctx := context.Background()

	f.do(http.MethodPost, "/api/todos", "title=Shared+board&visibility=public", a)
	f.do(http.MethodPost, "/api/todos", "title=Mine+only", b)
	ownerB := f.ownerOf(t, b)
	rows, _ := f.q.ListTodosByOwner(ctx, ownerB)
	if len(rows) != 1 {
		t.Fatalf("setup rows = %d, want 1", len(rows))
	}

	toggle := f.do(http.MethodPost, "/api/todos/"+rows[0].ID+"/toggle?filter=all&scope=mine", "", b)
	if toggle.Code != http.StatusOK {
		t.Fatalf("toggle: status = %d", toggle.Code)
	}
	tbody := toggle.Body.String()
	if !strings.Contains(tbody, "Mine only") {
		t.Error("mine-scoped toggle response must keep B's own row")
	}
	if strings.Contains(tbody, "Shared board") {
		t.Error("mine-scoped toggle response must not pull A's shared row back in")
	}
}

// TestCanDeleteRow pins the shared delete rule in one place: owner always
// (every role, viewer included), viewer never on shared, admin on any
// visible row, plain peer never.
func TestCanDeleteRow(t *testing.T) {
	mine := todo.Todo{ID: "1", Owner: "u1", Visibility: todo.VisibilityPrivate}
	shared := todo.Todo{ID: "2", Owner: "u1", Visibility: todo.VisibilityPublic}
	for _, tc := range []struct {
		name string
		role string
		me   string
		item todo.Todo
		want bool
	}{
		{"viewer own private", "viewer", "u1", mine, true},
		{"viewer own shared", "viewer", "u1", shared, true},
		{"viewer foreign shared", "viewer", "u2", shared, false},
		{"owner own private", "user", "u1", mine, true},
		{"owner own shared", "user", "u1", shared, true},
		{"peer shared", "user", "u2", shared, false},
		{"admin shared", "admin", "u9", shared, true},
		{"admin private others", "admin", "u9", mine, true},
	} {
		if got := canDeleteRow(tc.role, tc.me, tc.item); got != tc.want {
			t.Errorf("%s: canDeleteRow = %v, want %v", tc.name, got, tc.want)
		}
	}
}
