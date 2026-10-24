// SCOPE:feature — native panel in-place actions (red-proof).
//
// A chat answer renders the todo_list panel with the feature's REAL row
// actions (toggle, delete), each carrying the panel instance's id as
// _target. These tests pin the contract from the HTTP side: the row is
// written AND the response patches that slot (not the page's #todo-list),
// a forbidden delete stays rejected even with a slot, and a missing row
// refreshes the panel to the current truth instead of 404ing into a dead
// control. Reverting the _target branch in mutations.go fails all three:
// the responses patch #todo-list (or carry plain-text errors), which a
// host page without that region renders as nothing.
package handlers

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
	todoscomp "github.com/calionauta/gogogo/features/todo/components"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// panelOwnerID resolves the test user's id through its cookie.
func panelOwnerID(t *testing.T, f *handlersFixture, cookie *http.Cookie) string {
	t.Helper()
	req := newAuthRequest(cookie)
	sess, err := f.auth.Session(req)
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	return sess.User.ID
}

func newAuthRequest(cookie *http.Cookie) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	return req
}

// panelRowID creates one todo over HTTP and returns its id.
func panelRowID(t *testing.T, f *handlersFixture, cookie *http.Cookie, owner, title string) string {
	t.Helper()
	if rec := f.do(http.MethodPost, "/api/todos", "title="+title, cookie); rec.Code != http.StatusOK {
		t.Fatalf("create %s: status = %d (%s)", title, rec.Code, rec.Body.String())
	}
	rows, err := f.q.ListTodosByOwner(context.Background(), owner)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.Title == title {
			return r.ID
		}
	}
	t.Fatalf("created row %q not found", title)
	return ""
}

// TestPanelTogglePatchesPanelInPlace pins the acting tab's freshness: a
// toggle fired from a panel flips the row AND answers with a patch to the
// panel's own slot. The old code patched #todo-list unconditionally, which
// no host page but /todo owns — on /chat the click changed the store and
// the screen together read as "nothing happened".
func TestPanelTogglePatchesPanelInPlace(t *testing.T) {
	f := newHandlersFixture(t)
	ctx := context.Background()
	cookie := f.signup("panel-toggle@example.com")
	owner := panelOwnerID(t, f, cookie)
	id := panelRowID(t, f, cookie, owner, "Buy milk")

	const target = "genui-paneltoggle1"
	rec := f.do(http.MethodPost,
		"/api/todos/"+id+"/toggle?filter=all&_target="+target+"&clientID=chat-tab-1", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := html.UnescapeString(rec.Body.String())
	if !strings.Contains(body, "selector #"+target) {
		t.Errorf("toggle must patch the panel slot #target, got %.400q…", body)
	}
	if strings.Contains(body, "selector #todo-list") {
		t.Errorf("toggle from a panel must not patch the page list, got %.400q…", body)
	}
	if !strings.Contains(body, "Buy milk") {
		t.Errorf("toggled panel must still carry the row, got %.400q…", body)
	}

	rows, err := f.q.ListTodosByOwner(ctx, owner)
	if err != nil {
		t.Fatalf("re-list: %v", err)
	}
	for _, r := range rows {
		if r.ID == id && !r.Completed {
			t.Errorf("row %s still open after panel toggle", id)
		}
	}
}

// TestPanelDeletePatchesPanelInPlace pins the delete half: the row is gone
// from the store and the panel answers minus that row, at its own slot.
func TestPanelDeletePatchesPanelInPlace(t *testing.T) {
	f := newHandlersFixture(t)
	ctx := context.Background()
	cookie := f.signup("panel-delete@example.com")
	owner := panelOwnerID(t, f, cookie)
	keepID := panelRowID(t, f, cookie, owner, "keep me")
	dropID := panelRowID(t, f, cookie, owner, "drop me")

	const target = "genui-paneldelete1"
	rec := f.do(http.MethodPost,
		"/api/todos/"+dropID+"/delete?filter=all&_target="+target+"&clientID=chat-tab-1", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := html.UnescapeString(rec.Body.String())
	if !strings.Contains(body, "selector #"+target) {
		t.Errorf("delete must patch the panel slot #target, got %.400q…", body)
	}
	if strings.Contains(body, "drop me") {
		t.Errorf("deleted row must leave the panel, got %.400q…", body)
	}
	if !strings.Contains(body, "keep me") {
		t.Errorf("surviving row must stay in the panel, got %.400q…", body)
	}

	rows, err := f.q.ListTodosByOwner(ctx, owner)
	if err != nil {
		t.Fatalf("re-list: %v", err)
	}
	for _, r := range rows {
		if r.ID == dropID {
			t.Errorf("row %s still present after panel delete", dropID)
		}
		if r.ID == keepID && r.Title != "keep me" {
			t.Errorf("survivor changed: %+v", r)
		}
	}
}

// TestPanelDeleteForbiddenStaysRejected pins that _target buys no rights: a
// viewer deleting another login's shared row gets the in-place failure card
// (same slot, visible), never a panel re-render and never a silent 403 the
// @post cannot render. Enforcement stays server-side (canDeleteRow).
func TestPanelDeleteForbiddenStaysRejected(t *testing.T) {
	f := newHandlersFixture(t)
	ctx := context.Background()

	ownerCookie := f.signup("panel-maker@example.com")
	owner := panelOwnerID(t, f, ownerCookie)
	if rec := f.do(http.MethodPost, "/api/todos",
		"title=shared+probe&visibility=public", ownerCookie); rec.Code != http.StatusOK {
		t.Fatalf("share create status = %d", rec.Code)
	}

	viewerCookie := f.signup("panel-watcher@example.com")
	if err := f.q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "viewer", Updated: dbschema.Now(), Email: "panel-watcher@example.com",
	}); err != nil {
		t.Fatalf("set viewer role: %v", err)
	}

	// The shared row belongs to the maker; the viewer may see it, never
	// delete it.
	rows, err := f.q.ListTodosByOwner(ctx, owner)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var foreign string
	for _, r := range rows {
		if r.Title == "shared probe" {
			foreign = r.ID
		}
	}
	if foreign == "" {
		t.Fatal("shared row not found")
	}

	const target = "genui-paneldeny1"
	rec := f.do(http.MethodPost,
		"/api/todos/"+foreign+"/delete?filter=all&_target="+target+"&clientID=chat-tab-1", "", viewerCookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("forbidden delete status = %d, want 200 with the in-place card (%s)", rec.Code, rec.Body.String())
	}
	body := html.UnescapeString(rec.Body.String())
	if !strings.Contains(body, "selector #"+target) {
		t.Errorf("rejection must land on the clicked slot, got %.400q…", body)
	}
	if strings.Contains(body, "selector #todo-list") {
		t.Errorf("rejection must not patch the page list, got %.400q…", body)
	}

	// And the row survives.
	rows, err = f.q.ListTodosByOwner(ctx, owner)
	if err != nil {
		t.Fatalf("re-list: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.ID == foreign {
			found = true
		}
	}
	if !found {
		t.Errorf("forbidden delete removed row %s", foreign)
	}
}

// TestPanelToggleMissingRowRefreshesPanel pins the gone-elsewhere case: the
// row was deleted from another tab, so the toggle answers with the current
// list at the same slot — never a bare 404 the @post cannot render, and
// never invented data.
func TestPanelToggleMissingRowRefreshesPanel(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("panel-gone@example.com")
	owner := panelOwnerID(t, f, cookie)
	panelRowID(t, f, cookie, owner, "still here")

	const target = "genui-panelgone1"
	rec := f.do(http.MethodPost,
		"/api/todos/00000000-0000-0000-0000-000000000000/toggle?filter=all&_target="+target+"&clientID=chat-tab-1",
		"", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("missing-row toggle status = %d, want 200 with the refreshed panel (%s)", rec.Code, rec.Body.String())
	}
	body := html.UnescapeString(rec.Body.String())
	if !strings.Contains(body, "selector #"+target) {
		t.Errorf("missing row must refresh the panel slot, got %.400q…", body)
	}
	if !strings.Contains(body, "still here") {
		t.Errorf("refreshed panel must carry the surviving row, got %.400q…", body)
	}
}

// TestPanelRendersInteractiveControls pins the markup half: the panel owns
// real toggle/delete posts carrying its slot id and slice. The previous
// read-only panel rendered neither, so a revert of panel.templ fails here
// before any HTTP runs.
func TestPanelRendersInteractiveControls(t *testing.T) {
	const id = "genui-panelrender1"
	var buf strings.Builder
	rows := []todo.PanelRows{{ID: "row-1", Title: "Buy milk", Done: false}}
	if err := todoscomp.Panel(id, "all", rows).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render panel: %v", err)
	}
	got := html.UnescapeString(buf.String())
	for _, want := range []string{
		`id="` + id + `"`,
		"/api/todos/row-1/toggle",
		"/api/todos/row-1/delete",
		"_target=" + id,
		"filter=all",
		"Buy milk",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("panel markup missing %q:\n%s", want, got)
		}
	}
}
