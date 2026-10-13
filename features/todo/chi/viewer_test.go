// SCOPE:feature — Viewer-role UI contract tests (red-proof).
package chi

import (
	"context"
	"html"
	"net/http"
	"strings"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// TestTodoViewerDeleteAffordanceDisabled pins the RBAC display hint:
// a viewer sees aria-disabled trash + the denial message wiring, and
// the isViewer signal; a plain user sees the modal opener. Enforcement
// is NOT asserted here (policy.csv owns it, pinned in internal/rbac) —
// only that the UI narrates the role it was given.
func TestTodoViewerDeleteAffordanceDisabled(t *testing.T) {
	f := newChiFixture(t)
	ctx := context.Background()

	viewerCookie := f.signup("watcher-todo@example.com")
	if err := f.q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: "viewer", Updated: dbschema.Now(), Email: "watcher-todo@example.com",
	}); err != nil {
		t.Fatalf("set viewer role: %v", err)
	}

	userCookie := f.signup("maker-todo@example.com")

	// A trash button only renders with rows: the viewer creates one
	// (allowed), then re-reads the page.
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
	if !strings.Contains(viewBody, `aria-disabled="true"`) {
		t.Fatalf("viewer page must render disabled delete affordance")
	}
	if !strings.Contains(viewBody, "$permDenied = true") {
		t.Fatalf("viewer delete must wire the denial message")
	}
	if strings.Contains(viewBody, "showModal()") {
		t.Fatalf("viewer page must not render the modal-opening trash")
	}

	createUser := f.do(http.MethodPost, "/api/todos", "title=user+probe", userCookie)
	if createUser.Code != http.StatusOK {
		t.Fatalf("user create status = %d", createUser.Code)
	}
	userRec := f.do(http.MethodGet, "/todo", "", userCookie)
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
