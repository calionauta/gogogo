// The todo slice's half of the record.row-actions slot.
//
// A recipe declares an action as data; this file is where that data becomes
// something the reference slice can render. It exists because the conversion is
// the ONLY place that knows both sides: `features/todo` deliberately does not
// import the recipe engine (a trimmed build would drag it in), so the shape is
// translated here and nowhere else.
package handlers

import (
	"github.com/calionauta/gogogo/features/todo"
	"github.com/calionauta/gogogo/internal/recipe"
)

// RowActionsFrom converts the slot's contributions into the row's shape.
//
// Exported because the composition root copies the same list into the page's
// data: a second converter there would be two spellings of one contract, and the
// page and the panel would drift the first time either changed.
//
// Nil registry and empty slot both answer nil: a build with no installed recipe
// renders the row exactly as it did before the slot existed.
func RowActionsFrom(slots *recipe.SlotRegistry) []todo.RowAction {
	if slots == nil {
		return nil
	}
	actions := slots.Actions(recipe.SlotRecordRowActions)
	if len(actions) == 0 {
		return nil
	}
	out := make([]todo.RowAction, 0, len(actions))
	for _, a := range actions {
		out = append(out, todo.RowAction{
			Label:   a.Label,
			Href:    a.Href,
			Verb:    a.Verb,
			Confirm: a.Confirm,
		})
	}
	return out
}
