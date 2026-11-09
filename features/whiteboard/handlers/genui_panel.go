// The whiteboard feature's native panel: one file that makes the shared
// boards answerable from Chat. Same contract as the todo reference
// (features/todo/handlers/genui_panel.go): Render returns the feature's own
// list component, Fill owns the rows, the model picks only the slice.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/a-h/templ"

	wbcomponents "github.com/calionauta/gogogo/features/whiteboard/components"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

const (
	scopeAll  = "all"
	scopeMine = "mine"
)

// BoardsPanel is the app's own boards_list panel.
type BoardsPanel struct {
	Q *dbgen.Queries
}

// NewBoardsPanel builds the provider. Nil queries is a wiring bug, not a
// degradation: the whiteboard handler that owns them panics on nil too.
func NewBoardsPanel(q *dbgen.Queries) *BoardsPanel {
	if q == nil {
		panic("whiteboard: NewBoardsPanel with nil queries")
	}
	return &BoardsPanel{Q: q}
}

// Component implements comp.PanelProvider.
func (p *BoardsPanel) Component() string { return "boards_list" }

// Prompt implements comp.PanelProvider: when the model should pick it.
func (p *BoardsPanel) Prompt() string {
	return "boards_list {scope: all|mine} whenever the request involves the shared " +
		"whiteboards (list them, find a board, plan around them). Renders their " +
		"REAL rows, so it is the only component allowed to answer with board content. " +
		"scope: mine ONLY when the user asks for their own boards; all otherwise. " +
		"Always send scope inside props."
}

// HomePage implements comp.PanelHomer: the page that owns this data in full.
func (p *BoardsPanel) HomePage() string { return "/whiteboard" }

// Aliases implements comp.PanelAliaiser.
func (p *BoardsPanel) Aliases() []string { return []string{"boards", "board_list"} }

// Writable implements comp.PanelProvider: the model picks the scope only.
func (p *BoardsPanel) Writable() []string { return []string{"scope"} }

// Fill implements comp.PanelProvider. The index is shared — every signed-in
// user sees every board — so "all" needs no owner filter; "mine" keeps only
// the requester's docs. Delete rights come from boardMeta, the same stamp
// the index uses.
func (p *BoardsPanel) Fill(ctx context.Context, owner string, props map[string]any) (map[string]any, error) {
	scope, _ := props["scope"].(string)
	if scope != scopeAll && scope != scopeMine {
		return nil, fmt.Errorf("whiteboard panel: scope %q is not one of [all mine]", scope)
	}
	rows, err := p.Q.ListBoards(ctx)
	if err != nil {
		return nil, fmt.Errorf("whiteboard: list for genui panel: %w", err)
	}
	out := make([]wbcomponents.BoardMeta, 0, len(rows))
	for _, row := range rows {
		if scope == scopeMine && row.Owner != owner {
			continue
		}
		out = append(out, boardMeta(row, owner, ""))
	}
	return map[string]any{"scope": scope, "rows": out}, nil
}

// Render implements comp.PanelProvider: the feature's own list component.
func (p *BoardsPanel) Render(props map[string]any) (templ.Component, error) {
	scope, _ := props["scope"].(string)
	if scope != scopeAll && scope != scopeMine {
		return nil, fmt.Errorf("whiteboard panel: scope %q is not one of [all mine]", scope)
	}
	return wbcomponents.WhiteboardListFragment(panelBoardMetas(props)), nil
}

// panelBoardMetas decodes the `rows` prop in the shapes it reaches Render in
// (direct []BoardMeta or the wire []any of maps, via the row's own JSON tags).
func panelBoardMetas(props map[string]any) []wbcomponents.BoardMeta {
	switch rows := props["rows"].(type) {
	case []wbcomponents.BoardMeta:
		return rows
	case []any:
		out := make([]wbcomponents.BoardMeta, 0, len(rows))
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			blob, merr := json.Marshal(row)
			if merr != nil {
				continue
			}
			var item wbcomponents.BoardMeta
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
