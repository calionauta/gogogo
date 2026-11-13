// The coffee recipe's native panel: the app's own orders, answerable from Chat.
//
// It is built on comp.SlicePanel, the shared base (convention over
// configuration): the recipe supplies the name, the prompt, the one writable
// slice and two closures, and inherits Fill/Render. The view is
// components.OrdersList — the SAME component /coffee renders — so the panel
// cannot drift from the page it summarizes (the failure docs/native-panels.md
// records).
//
// No ActionProvider: the confirm response patches the page's `#coffee-orders`
// region, which a chat thread does not have, and an action that writes while
// looking inert is worse than no action. Headless callers use the declared
// InvokeAction instead, which returns the same signals without a DOM target.
package handlers

import (
	"context"
	"encoding/json"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/features/coffee/components"
	comp "github.com/calionauta/gogogo/features/genui/components"
)

const (
	// scopeRecent is the short recap; scopeAll is the page's own cap.
	scopeRecent = "recent"
	scopeAll    = "all"
)

// panel is the SlicePanel plus nothing: embedding keeps the inherited methods
// and lets this type stay a value, the same shape NewSlicePanel returns.
type panel struct {
	comp.SlicePanel
}

// Panel builds the chat provider. The closures capture the handler, so the
// panel reads the store through the same service the page does.
func (h *Handler) Panel() comp.PanelProvider {
	return panel{comp.NewSlicePanel(
		"coffee_orders",
		"coffee_orders {scope: recent|all} whenever the user asks about their coffee orders",
		"scope",
		[]string{scopeRecent, scopeAll},
		h.panelRows,
		panelRowsFromProps,
		func(rows []any, _ string) (templ.Component, error) {
			return components.OrdersList(decodePanelOrders(rows)), nil
		},
	)}
}

// panelRows is the List closure: the viewer's own rows, bounded by the slice the
// model chose. `recent` is a short recap; `all` is the page's own cap.
func (h *Handler) panelRows(ctx context.Context, owner, scope string) (any, error) {
	limit := ordersPageLimit
	if scope == scopeRecent {
		limit = 5
	}
	rows, err := h.orders.List(ctx, owner, limit)
	if err != nil {
		return nil, err
	}
	views := orderViews(rows)
	out := make([]any, 0, len(views))
	for _, v := range views {
		out = append(out, v)
	}
	return out, nil
}

// panelRowsFromProps reads the `rows` prop in the shapes it actually reaches
// Render in: []any from the wire (the answer crossed the queue as JSON) and
// []components.Order from a fill in this process.
func panelRowsFromProps(props map[string]any) []any {
	switch rows := props["rows"].(type) {
	case []any:
		return rows
	case []components.Order:
		out := make([]any, len(rows))
		for i := range rows {
			out[i] = rows[i]
		}
		return out
	}
	return nil
}

// decodePanelOrders turns the wire shape back into the page's view type. A row
// that does not decode is skipped rather than rendered blank: a panel with one
// missing order is a smaller lie than a row of empty labels.
func decodePanelOrders(rows []any) []components.Order {
	out := make([]components.Order, 0, len(rows))
	for _, raw := range rows {
		switch row := raw.(type) {
		case components.Order:
			out = append(out, row)
		case map[string]any:
			blob, err := json.Marshal(row)
			if err != nil {
				continue
			}
			var order components.Order
			if uerr := json.Unmarshal(blob, &order); uerr != nil {
				continue
			}
			out = append(out, order)
		}
	}
	return out
}
