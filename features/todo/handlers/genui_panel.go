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
//
// It names the CONCRETE cases (a listing, a filter, an "anything I still have
// to do?") rather than describing the component, because this sentence is the
// only place the model learns the panel exists and a vague line loses to the
// models' default of writing a plausible-looking list itself.
func (p *TodoPanel) Prompt() string {
	return "todo_list {filter: " + strings.Join(todo.ListFilters, "|") + "} whenever the " +
		"request involves the user's OWN todos or tasks (show/list/filter them, " +
		"check what is left, plan a day around them, summarize progress): it " +
		"renders their REAL rows, so it is the only component allowed to answer " +
		"with todo content"
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
	// Normalize first, then write the normalized value through: the renderer
	// re-validates (the history path reaches it without Fill), so storing the
	// raw spelling here would make Render reject the answer Fill accepted.
	filled := normalizedProps(props)
	filter, _ := filled["filter"].(string)
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
	filled["rows"] = out
	return filled, nil
}

// Summary implements comp.PanelSummarizer: the digest of the viewer's todos
// that lets the model REASON about them instead of inventing them.
//
// Distinct from Fill on purpose. Fill populates the panel's rendered rows;
// this produces prompt context, and the two have different budgets and
// audiences — rows are bounded by the page, the digest by the prompt, and the
// digest needs the open/done split spelled out in words because the model is
// planning, not just displaying. Open rows come first: "what is left" is what
// a plan is built from, and the prompt cap truncates the tail.
//
// An empty list returns no text at all rather than "you have no todos": the
// caller omits the whole block, so the model is never handed a negative fact it
// might report as an answer to a different question.
func (p *TodoPanel) Summary(ctx context.Context, owner string) (string, error) {
	rows, err := p.Q.ListTodosByOwner(ctx, owner)
	if err != nil {
		return "", fmt.Errorf("todo: summarize for genui prompt: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	open, done := 0, 0
	var b strings.Builder
	fmt.Fprintf(&b, "%d todo(s) total", len(rows))
	for _, r := range rows {
		if r.Completed {
			done++
		} else {
			open++
		}
	}
	fmt.Fprintf(&b, " (%d open, %d done).", open, done)
	if open > 0 {
		b.WriteString(" Open: ")
		b.WriteString(titles(rows, false))
	}
	if done > 0 {
		b.WriteString(" Done: ")
		b.WriteString(titles(rows, true))
	}
	return b.String(), nil
}

// titles joins the titles of one slice, separated by "; ".
func titles(rows []dbgen.Todo, done bool) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Completed == done {
			parts = append(parts, r.Title)
		}
	}
	return strings.Join(parts, "; ")
}

// normalizedProps copies props with the model's filter spelling mapped onto
// the closed set, so validation sees the value the store will be asked for.
func normalizedProps(props map[string]any) map[string]any {
	raw, _ := props["filter"].(string)
	out := make(map[string]any, len(props)+1)
	maps.Copy(out, props)
	out["filter"] = todo.NormalizeFilter(raw)
	return out
}

// Render implements comp.PanelProvider: the filled props become the panel's
// component. It re-validates the enum because Render is also reached by the
// history path (a stored answer re-renders through here), so it cannot assume
// Fill ran in this process.
func (p *TodoPanel) Render(props map[string]any) (templ.Component, error) {
	normalized := normalizedProps(props)
	filter, _ := normalized["filter"].(string)
	if !todo.FilterValid(filter) {
		return nil, fmt.Errorf("todo panel: filter %q is not one of %v", filter, todo.ListFilters)
	}
	rows, _ := props["rows"].([]todo.PanelRows)
	return todoscomp.Panel(filter, rows), nil
}
