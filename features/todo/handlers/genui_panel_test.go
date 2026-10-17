// SCOPE:feature — the genui panel seam's contract (rows, filter, scoping).
package handlers

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// TestTodoListListerFiltersAndScopes pins the two things a system panel must
// not get wrong: it answers with the VIEWER's own rows (ListTodosByOwner,
// never the visibility-scoped page query, so a panel cannot become a window
// onto someone else's shared board), and each filter means what the catalog
// enum says. Red-proof: swap ListTodosByOwner for the visible query and the
// other user's public row shows up in "my todos".
func TestTodoListListerFiltersAndScopes(t *testing.T) {
	f := newHandlersFixture(t)
	ctx := context.Background()
	meID := f.ownerOf(t, f.signup("lister@example.com"))
	otherID := f.ownerOf(t, f.signup("stranger@example.com"))

	create := func(owner string, title, visibility string) {
		t.Helper()
		if _, err := f.q.CreateTodo(ctx, dbgen.CreateTodoParams{
			ID: dbschema.NewID(), Owner: owner, Title: title, Visibility: visibility,
		}); err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
	}
	create(meID, "mine open", todo.VisibilityPrivate)
	create(meID, "mine done", todo.VisibilityPrivate)
	create(otherID, "theirs public", todo.VisibilityPublic)
	if err := f.q.SetTodoCompleted(ctx, dbgen.SetTodoCompletedParams{
		ID: listRowID(t, f, meID, "mine done"), Owner: meID, Completed: true,
	}); err != nil {
		t.Fatalf("complete: %v", err)
	}

	p := NewTodoPanel(f.q)
	if got := p.Component(); got != "todo_list" {
		t.Fatalf("Component = %q, want todo_list (the catalog's registry key)", got)
	}
	for filter, want := range map[string]int{"open": 1, "done": 1, "all": 2} {
		props, err := p.Fill(ctx, meID, map[string]any{"filter": filter})
		if err != nil {
			t.Fatalf("%s: %v", filter, err)
		}
		rows, _ := props["rows"].([]todo.PanelRows)
		if len(rows) != want {
			t.Errorf("%s: %d rows, want %d (%+v)", filter, len(rows), want, rows)
		}
		for _, r := range rows {
			if r.Title == "theirs public" {
				t.Errorf("%s: another owner's row leaked into my panel", filter)
			}
		}
	}
	// A filter outside the closed set is refused here too, not just at parse
	// time: the store must never receive a string a model chose.
	if _, err := p.Fill(ctx, meID, map[string]any{"filter": "everything"}); err == nil {
		t.Error("an unknown filter was accepted by the panel")
	}
}

// listRowID finds one row's id by title, so the completion step does not
// need the create call to return it.
func listRowID(t *testing.T, f *handlersFixture, owner, title string) string {
	t.Helper()
	rows, err := f.q.ListTodosByOwner(context.Background(), owner)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, r := range rows {
		if r.Title == title {
			return r.ID
		}
	}
	t.Fatalf("row %q not found for %s", title, owner)
	return ""
}
