// SCOPE:feature — The genui panel's data seam (Chat <-> todos).
//
// This path had no test, which is how "show my todos" could answer with an empty
// panel and look like a model failure. Fill is the seam: it reads the viewer's
// rows and narrows them by the filter the model chose.
package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	comp "github.com/calionauta/gogogo/features/genui/components"
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

	rows := func(props map[string]any) []todo.Todo {
		t.Helper()
		out, ferr := p.Fill(ctx, owner, props)
		if ferr != nil {
			t.Fatalf("Fill(%v): %v", props, ferr)
		}
		got, ok := out["rows"].([]todo.Todo)
		if !ok {
			t.Fatalf("Fill(%v) returned no rows slice: %#v", props, out)
		}
		return got
	}

	if open := rows(map[string]any{"filter": "open"}); len(open) != 1 || open[0].Title != "open one" {
		t.Fatalf("filter=open rows = %+v, want just the open one", open)
	}
	// The chat composer's own example is "what are my open todos?", and a model
	// that omits the prop used to get the ALL slice, completed rows included.
	if omitted := rows(map[string]any{}); len(omitted) != 1 || omitted[0].Title != "open one" {
		t.Fatalf("omitted filter rows = %+v, want the open slice", omitted)
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
	got, ok := out["rows"].([]todo.Todo)
	if !ok {
		t.Fatalf("Fill for another owner returned no rows slice: %#v", out)
	}
	if len(got) != 0 {
		t.Fatalf("another owner saw %d rows", len(got))
	}
}

// TestTodoPanelRendersOfficialRows pins that the native answer is the
// feature's OWN component with the viewer's REAL rows — not an invented
// list. Drop the Fill call from the worker and the titles go missing; let
// the model write its own rows and there is no "Open in Todos" home link.
// Both halves are what separate a native panel from a plausible fake.
func TestTodoPanelRendersOfficialRows(t *testing.T) {
	f := newHandlersFixture(t)
	ctx := context.Background()
	f.signup("render@example.com")
	user, err := f.h.Q.GetUserByEmail(ctx, "render@example.com")
	if err != nil {
		t.Fatalf("get seeded user: %v", err)
	}
	now := dbschema.Now()
	if _, cerr := f.h.Q.CreateTodo(ctx, dbgen.CreateTodoParams{
		ID: "todo-real", Owner: user.ID, Title: "buy milk", Completed: false, IdemKey: "",
		Visibility: todo.VisibilityPrivate, Created: now, Updated: now,
	}); cerr != nil {
		t.Fatalf("create todo: %v", cerr)
	}

	p := NewTodoPanel(f.h.Q)
	filled, ferr := p.Fill(ctx, user.ID, map[string]any{"filter": "all"})
	if ferr != nil {
		t.Fatalf("Fill: %v", ferr)
	}
	rendered, rerr := p.Render(filled)
	if rerr != nil {
		t.Fatalf("Render: %v", rerr)
	}
	var buf strings.Builder
	if err := rendered.Render(ctx, &buf); err != nil {
		t.Fatalf("Render template: %v", err)
	}
	html := buf.String()
	for _, want := range []string{"buy milk", "Open in Todos", "/todo"} {
		if !strings.Contains(html, want) {
			t.Errorf("native panel missing %q (got %.300q…)", want, html)
		}
	}
	if strings.Contains(html, "<script") {
		t.Errorf("native panel carries script markup: %.200q…", html)
	}
}

// TestTodoPanelRenderDecodesWireRows is the LIVE path, and it is the one that
// shipped broken: the worker hydrates the panel, the answer crosses the queue
// and the SSE wire as JSON, and encoding/json turns the typed rows slice into
// []any of map[string]any. Render's typed assertion then failed, the panel
// painted "No todos to show here", and a page reload — which renders the
// stored typed props — showed the rows, which read as "the live answer lost
// my todos until I refreshed".
//
// Red-proof: drop the []any arm from panelRowsFromProps and the two titles
// below disappear.
func TestTodoPanelRenderDecodesWireRows(t *testing.T) {
	f := newHandlersFixture(t)
	ctx := context.Background()
	f.signup("wire@example.com")
	user, err := f.h.Q.GetUserByEmail(ctx, "wire@example.com")
	if err != nil {
		t.Fatalf("get seeded user: %v", err)
	}
	now := dbschema.Now()
	for id, title := range map[string]string{"todo-open-wire": "still open", "todo-done-wire": "already done"} {
		if _, cerr := f.h.Q.CreateTodo(ctx, dbgen.CreateTodoParams{
			ID: id, Owner: user.ID, Title: title,
			Completed: id == "todo-done-wire", IdemKey: "",
			Visibility: todo.VisibilityPrivate, Created: now, Updated: now,
		}); cerr != nil {
			t.Fatalf("create %q: %v", title, cerr)
		}
	}

	p := NewTodoPanel(f.h.Q)
	filled, ferr := p.Fill(ctx, user.ID, map[string]any{"filter": "open"})
	if ferr != nil {
		t.Fatalf("Fill: %v", ferr)
	}
	// Exactly what the worker sends and the stream dispatcher decodes.
	blob, merr := json.Marshal([]comp.Directive{{
		Component: "todo_list",
		Props:     filled,
	}})
	if merr != nil {
		t.Fatalf("marshal wire directive: %v", merr)
	}
	var wire []comp.Directive
	if uerr := json.Unmarshal(blob, &wire); uerr != nil {
		t.Fatalf("decode wire directive: %v", uerr)
	}
	rendered, rerr := p.Render(wire[0].Props)
	if rerr != nil {
		t.Fatalf("Render(wire props): %v", rerr)
	}
	var buf strings.Builder
	if err := rendered.Render(ctx, &buf); err != nil {
		t.Fatalf("Render template: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "still open") {
		t.Errorf("live panel lost the hydrated row (got %.400q…)", html)
	}
	if strings.Contains(html, "No todos to show here") {
		t.Errorf("live panel rendered its empty state over real rows: %.400q…", html)
	}
	if strings.Contains(html, "already done") {
		t.Errorf("filter=open rendered a completed row: %.400q…", html)
	}
}
