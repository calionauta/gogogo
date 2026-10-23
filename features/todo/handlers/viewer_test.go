// SCOPE:feature — Viewer-role UI contract tests (red-proof).
package handlers

import (
	"context"
	"html"
	"net/http"
	"strings"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/llm"
)

// TestTodoViewerDeleteAffordanceDisabled pins the RBAC display hint:
// a viewer has full rights over their own rows (modal-opening trash,
// like any owner) but sees aria-disabled trash + the denial wiring on
// shared rows they don't own. Enforcement is NOT asserted here
// (policy.csv owns the verb, canDeleteRow owns the row — pinned in
// internal/rbac and the sharing tests) — only that the UI narrates
// what the server stamped on each row.
func TestTodoViewerDeleteAffordanceDisabled(t *testing.T) {
	f := newHandlersFixture(t)
	ctx := context.Background()

	viewerCookie := f.signup("watcher-todo@example.com")
	if err := f.q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "viewer", Updated: dbschema.Now(), Email: "watcher-todo@example.com",
	}); err != nil {
		t.Fatalf("set viewer role: %v", err)
	}

	userCookie := f.signup("maker-todo@example.com")
	// The other login shares one row so the viewer has a foreign shared
	// row to look at (viewer restrictions apply to shared, never own).
	if rec := f.do(http.MethodPost, "/api/todos",
		"title=shared+probe&visibility=public", userCookie); rec.Code != http.StatusOK {
		t.Fatalf("user share create status = %d (%s)", rec.Code, rec.Body.String())
	}

	// The viewer creates their own row (allowed), then re-reads the page.
	createRec := f.do(http.MethodPost, "/api/todos", "title=viewer+probe", viewerCookie)
	if createRec.Code != http.StatusOK {
		t.Fatalf("viewer create status = %d (%s)", createRec.Code, createRec.Body.String())
	}
	viewRec := f.do(http.MethodGet, "/todo", "", viewerCookie)
	if viewRec.Code != http.StatusOK {
		t.Fatalf("viewer /todo status = %d", viewRec.Code)
	}
	viewBody := html.UnescapeString(viewRec.Body.String())
	if !strings.Contains(viewBody, `"isViewer":true`) {
		t.Fatalf("viewer page must seed isViewer:true")
	}
	// Own row: modal-opening trash, like any owner.
	if !strings.Contains(viewBody, "showModal()") {
		t.Fatalf("viewer page must render the modal-opening trash for their own row")
	}
	// Foreign shared row: disabled affordance + denial wiring instead.
	if !strings.Contains(viewBody, `aria-disabled="true"`) {
		t.Fatalf("viewer page must render disabled delete affordance on shared rows")
	}
	if !strings.Contains(viewBody, "$permDenied = true") {
		t.Fatalf("viewer delete must wire the denial message")
	}

	createUser := f.do(http.MethodPost, "/api/todos", "title=user+probe", userCookie)
	if createUser.Code != http.StatusOK {
		t.Fatalf("user create status = %d", createUser.Code)
	}
	userRec := f.do(http.MethodGet, "/todo", "", userCookie, demoCookie())
	if userRec.Code != http.StatusOK {
		t.Fatalf("user /todo status = %d", userRec.Code)
	}
	userBody := html.UnescapeString(userRec.Body.String())
	if !strings.Contains(userBody, `"isViewer":false`) {
		t.Fatalf("plain user page must seed isViewer:false")
	}
	if !strings.Contains(userBody, "showModal()") {
		t.Fatalf("plain user page must render the modal-opening trash")
	}
}

// TestTodoSuggestTabVisibility pins the LLM signal wiring the viewer
// gate depends on: a configured LLM shows AI Suggest to plain users
// and hides it from viewers. Red-proof: without the render-site
// assignment this fails on the first assertion (the tab never shows).
func TestTodoSuggestTabVisibility(t *testing.T) {
	f := newHandlersFixture(t)
	f.h.SetLLMClient(llm.New("test-key"))

	userCookie := f.signup("sugg-user@example.com")
	userRec := f.do(http.MethodGet, "/todo", "", userCookie, demoCookie())
	if userRec.Code != http.StatusOK {
		t.Fatalf("user /todo status = %d", userRec.Code)
	}
	if !strings.Contains(userRec.Body.String(), `>AI Suggest</button>`) {
		t.Fatalf("configured LLM must render the AI Suggest tab")
	}

	viewerCookie := f.signup("sugg-viewer@example.com")
	if err := f.q.SetUserRole(context.Background(), dbgen.SetUserRoleParams{
		Role: "viewer", Updated: dbschema.Now(), Email: "sugg-viewer@example.com",
	}); err != nil {
		t.Fatalf("set viewer role: %v", err)
	}
	viewRec := f.do(http.MethodGet, "/todo", "", viewerCookie)
	if viewRec.Code != http.StatusOK {
		t.Fatalf("viewer /todo status = %d", viewRec.Code)
	}
	// The label also lives in an HTML comment, so assert the gated
	// affordances (tab button + panel), not the bare string.
	for _, gated := range []string{`>AI Suggest</button>`, `data-show="$sidebarTab === 'ai'"`} {
		if strings.Contains(viewRec.Body.String(), gated) {
			t.Fatalf("viewer must not see the AI Suggest tab (paid action): found %q", gated)
		}
	}
}
