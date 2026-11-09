// The notes feature's native panel: one file that makes the shared notes
// answerable from Chat. Same contract as the todo reference
// (features/todo/handlers/genui_panel.go): Render returns the feature's own
// list component, Fill owns the rows, the model picks only the slice.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/a-h/templ"

	notescomp "github.com/calionauta/gogogo/features/notes/components"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

const (
	scopeAll  = "all"
	scopeMine = "mine"
)

// NotesPanel is the app's own notes_list panel.
type NotesPanel struct {
	Q *dbgen.Queries
}

// NewNotesPanel builds the provider. Nil queries is a wiring bug, not a
// degradation: the notes handler that owns them panics on nil too.
func NewNotesPanel(q *dbgen.Queries) *NotesPanel {
	if q == nil {
		panic("notes: NewNotesPanel with nil queries")
	}
	return &NotesPanel{Q: q}
}

// Component implements comp.PanelProvider.
func (p *NotesPanel) Component() string { return "notes_list" }

// Prompt implements comp.PanelProvider: when the model should pick it.
func (p *NotesPanel) Prompt() string {
	return "notes_list {scope: all|mine} whenever the request involves the user's " +
		"shared notes (list them, find a note's title, plan around them, summarize). " +
		"Renders their REAL rows, so it is the only component allowed to answer " +
		"with note content. scope: mine ONLY when the user asks for their own notes; " +
		"all otherwise. Always send scope inside props."
}

// HomePage implements comp.PanelHomer: the page that owns this data in full.
func (p *NotesPanel) HomePage() string { return "/notes" }

// Aliases implements comp.PanelAliaiser.
func (p *NotesPanel) Aliases() []string { return []string{"notes", "note_list"} }

// Writable implements comp.PanelProvider: the model picks the scope only.
func (p *NotesPanel) Writable() []string { return []string{propScope} }

// Fill implements comp.PanelProvider. The index is shared — every signed-in
// user sees every note — so "all" needs no owner filter; "mine" keeps only
// the requester's docs. Delete rights come from canDeleteDoc with an empty
// role (owner-only), the same conservative read the todo panel stamps.
func (p *NotesPanel) Fill(ctx context.Context, owner string, props map[string]any) (map[string]any, error) {
	scope, _ := props["scope"].(string)
	if scope != scopeAll && scope != scopeMine {
		return nil, fmt.Errorf("notes panel: scope %q is not one of [all mine]", scope)
	}
	rows, err := p.Q.ListNotes(ctx)
	if err != nil {
		return nil, fmt.Errorf("notes: list for genui panel: %w", err)
	}
	out := make([]notescomp.NoteMeta, 0, len(rows))
	for _, row := range rows {
		if scope == scopeMine && row.Owner != owner {
			continue
		}
		out = append(out, notescomp.NoteMeta{
			DocID:     row.DocID,
			Author:    row.OwnerEmail,
			IsMine:    owner != "" && row.Owner == owner,
			CanDelete: canDeleteDoc("", owner, row.Owner),
		})
	}
	filled := map[string]any{"scope": scope, "rows": out}
	return filled, nil
}

// Render implements comp.PanelProvider: the feature's own list component.
func (p *NotesPanel) Render(props map[string]any) (templ.Component, error) {
	scope, _ := props["scope"].(string)
	if scope != scopeAll && scope != scopeMine {
		return nil, fmt.Errorf("notes panel: scope %q is not one of [all mine]", scope)
	}
	rows := panelNoteMetas(props)
	return notescomp.NotesListFragment(rows), nil
}

// panelNoteMetas decodes the `rows` prop in the shapes it reaches Render in
// (direct []NoteMeta or the wire []any of maps, via the row's own JSON tags).
func panelNoteMetas(props map[string]any) []notescomp.NoteMeta {
	switch rows := props["rows"].(type) {
	case []notescomp.NoteMeta:
		return rows
	case []any:
		out := make([]notescomp.NoteMeta, 0, len(rows))
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			blob, merr := json.Marshal(row)
			if merr != nil {
				continue
			}
			var item notescomp.NoteMeta
			if uerr := json.Unmarshal(blob, &item); uerr != nil {
				continue
			}
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}
