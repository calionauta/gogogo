// SCOPE:feature — the todo feature's native panel: one file that makes the
// app's own todos answerable from Ask AI and Chat.
//
// This is the CONTRACT for becoming a native panel — a feature contributes a
// PanelProvider and nothing else. Name, prompt line, writable props, data and
// rendering all live here, next to the feature that owns the page, so adding a
// panel never means editing a shared map in features/genui.
//
// To remove: delete with features/todo/ (the genui unit lists this file).
package handlers

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/calionauta/gogogo/features/todo"
	todoscomp "github.com/calionauta/gogogo/features/todo/components"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"

	"github.com/a-h/templ"
)

// TodoPanel is the app's own todo_list panel.
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

// Component implements comp.PanelProvider: the model-facing name.
func (p *TodoPanel) Component() string { return "todo_list" }

// Prompt implements comp.PanelProvider: when the model should pick this panel.
func (p *TodoPanel) Prompt() string {
	return "todo_list {filter: " + strings.Join(todo.ListFilters, "|") + "} when the " +
		"request is about the user's OWN todos: it shows their real list, which " +
		"invented markup cannot"
}

// Writable implements comp.PanelProvider: the model picks the SLICE and
// nothing else. Rows are filled by Fill, never by the model.
func (p *TodoPanel) Writable() []string { return []string{"filter"} }

// Fill implements comp.PanelProvider: replaces the model's props with the
// viewer's rows. The filter is the catalog's validated enum, mapped here so
// the store never parses a string that came from a model; the query is
// ListTodosByOwner — deliberately not the page's visibility-scoped query,
// because a panel answering "my todos" must never become a window onto
// someone else's shared board.
func (p *TodoPanel) Fill(ctx context.Context, owner string, props map[string]any) (map[string]any, error) {
	filter, _ := props["filter"].(string)
	if !todo.FilterValid(filter) {
		return nil, fmt.Errorf("todo panel: filter %q is not one of %v", filter, todo.ListFilters)
	}
	rows, err := p.Q.ListTodosByOwner(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("todo: list for genui panel: %w", err)
	}
	out := make([]todo.PanelRows, 0, len(rows))
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
		out = append(out, todo.PanelRows{ID: r.ID, Title: r.Title, Done: r.Completed})
	}
	filled := make(map[string]any, len(props)+1)
	maps.Copy(filled, props)
	filled["rows"] = out
	return filled, nil
}

// Render implements comp.PanelProvider: the filled props become the panel's
// component. It re-validates the enum because Render is also reached by the
// history path (a stored answer re-renders through here), so it cannot assume
// Fill ran in this process.
func (p *TodoPanel) Render(props map[string]any) (templ.Component, error) {
	filter, _ := props["filter"].(string)
	if !todo.FilterValid(filter) {
		return nil, fmt.Errorf("todo panel: filter %q is not one of %v", filter, todo.ListFilters)
	}
	rows, _ := props["rows"].([]todo.PanelRows)
	return todoscomp.Panel(filter, rows), nil
}
