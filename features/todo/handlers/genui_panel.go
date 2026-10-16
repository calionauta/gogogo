// SCOPE:feature — the genui hydration seam, implemented by the feature that
// owns the rows. To remove: delete with features/todo/.
package handlers

import (
	"context"
	"fmt"

	comp "github.com/calionauta/gogogo/features/genui/components"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// TodoListLister answers the genui catalog's panel seam with the viewer's
// OWN rows. It is deliberately ListTodosByOwner and not the page's
// visibility-scoped query: a panel that answers "my todos" must never
// become a window onto someone else's shared board, and the owner predicate
// is the whole authorization story (no role, no visibility check to get
// wrong).
//
// The filter is the catalog's validated enum (components.todoListFilters),
// mapped here so the store never parses a string from a model.
type TodoListLister struct {
	Q *dbgen.Queries
}

// NewTodoListLister builds the seam. Nil queries is a wiring bug, not a
// degradation: the handler that owns them panics on nil too.
func NewTodoListLister(q *dbgen.Queries) *TodoListLister {
	if q == nil {
		panic("todo: NewTodoListLister with nil queries")
	}
	return &TodoListLister{Q: q}
}

// ListFor returns the viewer's rows for one filter. "all" is both halves;
// "open"/"done" filter on completion in Go, not in SQL, because the page
// query returns the owner's rows in one trip and the panel is a handful of
// them.
func (l *TodoListLister) ListFor(ctx context.Context, owner, filter string) ([]comp.TodoRow, error) {
	rows, err := l.Q.ListTodosByOwner(ctx, owner)
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
	return out, nil
}
