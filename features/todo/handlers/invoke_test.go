// The headless bridge, proved on a real slice: resolve the todo unit's
// declared `create` verb and run it against the fixture's real mux under a
// real session. The row must land in ext_todo_todos — the same table the
// carry test asserts.
package handlers

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/recipe"
)

// TestInvokeTodoCreateHeadless proves the dispatcher end to end: the verb
// resolves through the declaration, the session travels as the ordinary
// cookie, and the owned table gains the row.
func TestInvokeTodoCreateHeadless(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	cookie := f.signup("invoker@example.com")

	reg := &recipe.Registry{}
	reg.Add(f.h.Recipe())
	route, err := recipe.ResolveAction(reg, "todo", "create")
	if err != nil {
		t.Fatalf("resolve create: %v", err)
	}

	var before int
	if qerr := f.sqlDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM ext_todo_todos`).Scan(&before); qerr != nil {
		t.Fatalf("count before: %v", qerr)
	}
	code, _, err := recipe.Invoke(context.Background(), f.r, auth.CookieName, cookie.Value, route,
		url.Values{"title": {"Headless todo"}})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("invoke status %d, want 200/201", code)
	}
	var after int
	if err := f.sqlDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM ext_todo_todos WHERE title = 'Headless todo'`).Scan(&after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != 1 {
		t.Fatalf("owned table has %d 'Headless todo' rows, want 1", after)
	}
}

// TestInvokeTodoWithoutSessionFailsBeforeStore proves the anonymous path
// never writes: no cookie, no row.
func TestInvokeTodoWithoutSessionFailsBeforeStore(t *testing.T) {
	t.Parallel()
	f := newHandlersFixture(t)
	reg := &recipe.Registry{}
	reg.Add(f.h.Recipe())
	route, err := recipe.ResolveAction(reg, "todo", "create")
	if err != nil {
		t.Fatalf("resolve create: %v", err)
	}
	code, _, err := recipe.Invoke(context.Background(), f.r, auth.CookieName, "", route,
		url.Values{"title": {"Anon todo"}})
	if err == nil {
		t.Fatal("anonymous invoke must error")
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous invoke code %d, want 401", code)
	}
	var n int
	if err := f.sqlDB.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM ext_todo_todos WHERE title = 'Anon todo'`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("anonymous invoke wrote %d rows", n)
	}
}
