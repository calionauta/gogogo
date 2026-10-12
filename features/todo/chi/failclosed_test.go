// SCOPE:feature — "an unreadable list is not an empty list" contract tests.
//
// Each of these handlers used to swallow a list failure into an empty
// collection and render it, so a broken store looked exactly like "you have
// nothing yet" — to the user indistinguishable from the truth, and to the
// operator invisible. The red-proof case: with the store made to fail, the
// response must be an error, not a 200 with an empty list.
package chi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestChiIndexFailsClosedWhenListFails proves the todo page reports a broken
// store instead of rendering an empty list.
func TestChiIndexFailsClosedWhenListFails(t *testing.T) {
	f := newChiFixture(t)
	cookie := f.signup("failclosed@example.com")
	f.do(http.MethodPost, "/api/todos", "title=Real+item&filter=all", cookie)

	// Drop only the todos table: the session still resolves through users and
	// sessions, so the request reaches the list and the list alone fails.
	// (Closing the whole store would bounce to /login first, which is correct
	// but tests nothing about the list.)
	if _, err := f.sqlDB.ExecContext(context.Background(), "DROP TABLE todos"); err != nil {
		t.Fatalf("drop todos table: %v", err)
	}

	rec := f.do(http.MethodGet, "/todo", "", cookie)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /todo with a broken store: status = %d, want 500 (body: %s)",
			rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); strings.Contains(body, "Real item") {
		t.Fatalf("the broken-store page still rendered a todo (body: %s)", body)
	}
}
