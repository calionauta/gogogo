// SCOPE:feature — The genui panel's data seam (chat/Ask AI <-> todos).
//
// This path had no test, which is how "show my todos" could answer with an empty
// panel and look like a model failure. Fill is the seam: it reads the viewer's
// rows and narrows them by the filter the model chose.
package handlers

import (
	"context"
	"testing"

	"github.com/calionauta/gogogo/features/todo"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestTodoPanelFillListsTheOwnersRows(t *testing.T) {
	f := newHandlersFixture(t)
	ctx := context.Background()
	// todos.owner is a foreign key on users(id): the panel is asked with the
	// session's user id, so the fixture has to create a real account.
	f.signup("panel@example.com")
	user, err := f.h.Q.GetUserByEmail(ctx, "panel@example.com")
	if err != nil {
		t.Fatalf("get seeded user: %v", err)
	}
	owner := user.ID
	now := dbschema.Now()

	add := func(id, title string, done bool) {
		t.Helper()
		if _, cerr := f.h.Q.CreateTodo(ctx, dbgen.CreateTodoParams{
			ID: id, Owner: owner, Title: title, Completed: done, IdemKey: "",
			Visibility: todo.VisibilityPrivate, Created: now, Updated: now,
		}); cerr != nil {
			t.Fatalf("create %q: %v", title, cerr)
		}
	}
	add("todo-open", "open one", false)
	add("todo-done", "done one", true)

	p := NewTodoPanel(f.h.Q)

	rows := func(props map[string]any) []todo.PanelRows {
		t.Helper()
		out, ferr := p.Fill(ctx, owner, props)
		if ferr != nil {
			t.Fatalf("Fill(%v): %v", props, ferr)
		}
		got, ok := out["rows"].([]todo.PanelRows)
		if !ok {
			t.Fatalf("Fill(%v) returned no rows slice: %#v", props, out)
		}
		return got
	}

	if open := rows(map[string]any{"filter": "open"}); len(open) != 1 || open[0].Title != "open one" {
		t.Fatalf("filter=open rows = %+v, want just the open one", open)
	}
	if all := rows(map[string]any{"filter": "all"}); len(all) != 2 {
		t.Fatalf("filter=all rows = %d, want 2", len(all))
	}
	if done := rows(map[string]any{"filter": "done"}); len(done) != 1 || done[0].Title != "done one" {
		t.Fatalf("filter=done rows = %+v", done)
	}
	// The model's spelling is normalised before validation, so an alias must not
	// come back as "not one of the filters".
	if alias := rows(map[string]any{"filter": "completed"}); len(alias) != 1 {
		t.Fatalf("filter=completed (alias) rows = %d, want 1", len(alias))
	}

	// Another owner's rows never leak into the answer.
	out, err := p.Fill(ctx, user.ID+"-other", map[string]any{"filter": "all"})
	if err != nil {
		t.Fatalf("Fill for another owner: %v", err)
	}
	got, ok := out["rows"].([]todo.PanelRows)
	if !ok {
		t.Fatalf("Fill for another owner returned no rows slice: %#v", out)
	}
	if len(got) != 0 {
		t.Fatalf("another owner saw %d rows", len(got))
	}
}
