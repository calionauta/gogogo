// SCOPE:feature — the todo feature's contribution to the genui catalog: one
// provider that owns its name, its prompt line and its rows.
// To remove: delete with features/todo/.
package handlers

import (
	"context"
	"fmt"
	"maps"

	comp "github.com/calionauta/gogogo/features/genui/components"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// TodoPanel is the DATA half of the app's todo_list chat component: the
// catalog owns the name and the prompt line (so the advertisement needs no
// wiring), this fills the rows.
type TodoPanel struct {
	Q *dbgen.Queries
}

// NewTodoPanel builds the provider. Nil queries is a wiring bug, not a
// degradation: the todo handler that owns them panics on nil too.
func NewTodoPanel(q *dbgen.Queries) *TodoPanel {
	if q == nil {
		panic("todo: NewTodoPanel with nil queries")
	}
	return &TodoPanel{Q: q}
}

// Component implements comp.PanelProvider.
func (p *TodoPanel) Component() string { return "todo_list" }

// Fill replaces the model's props with the viewer's rows. The filter is the
// catalog's validated enum, mapped here so the store never parses a string
// that came from a model; the query is ListTodosByOwner — deliberately not
// the page's visibility-scoped query, because a panel answering "my todos"
// must never become a window onto someone else's shared board.
func (p *TodoPanel) Fill(ctx context.Context, owner string, props map[string]any) (map[string]any, error) {
	filter, _ := props["filter"].(string)
	if !comp.TodoFiltersContain(filter) {
		return nil, fmt.Errorf("todo panel: filter %q is not one of %v", filter, comp.TodoListFilters)
	}
	rows, err := p.Q.ListTodosByOwner(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("todo: list for genui panel: %w", err)
	}
	out := make([]comp.TodoRow, 0, len(rows))
	for _, r := range rows {
		switch filter {
		case "open":
			if r.Completed {
				continue
			}
		case "done":
			if !r.Completed {
				continue
			}
		}
		out = append(out, comp.TodoRow{ID: r.ID, Title: r.Title, Done: r.Completed})
	}
	filled := make(map[string]any, len(props)+1)
	maps.Copy(filled, props)
	filled["rows"] = out
	return filled, nil
}
