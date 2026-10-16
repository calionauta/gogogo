// SCOPE:plugin — hydration seam for the app's own panel components.
package components

import (
	"context"
	"maps"
)

// TodoLister is the seam that fills a system component's rows from the
// app's own store. It lives at the consumer: the catalog needs rows, the
// store owns them, and this package must not learn what a todo is (no
// dbgen, no RBAC import). The todo feature implements it and chi.go wires
// it.
type TodoLister interface {
	// ListFor returns the viewer's OWN rows for one validated filter.
	ListFor(ctx context.Context, owner, filter string) ([]TodoRow, error)
}

// Hydrate fills every system component's rows from the store.
//
// A system component with no lister is DROPPED, not rendered empty: an
// empty panel would tell the user "you have no todos", which is a lie the
// app must not tell when it simply was not wired (a trimmed build). A
// component whose lister errors is also dropped, and the error is returned
// for the caller to log — the answer keeps its other components either way,
// because losing a panel must not lose the answer.
func Hydrate(ctx context.Context, dirs []Directive, owner string, l TodoLister) ([]Directive, error) {
	var firstErr error
	kept := make([]Directive, 0, len(dirs))
	for _, d := range dirs {
		if d.Component != todoListName {
			kept = append(kept, d)
			continue
		}
		if l == nil {
			continue
		}
		filter, _ := d.Props["filter"].(string)
		rows, err := l.ListFor(ctx, owner, filter)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// Clone the props before filling: the caller's map is not ours to
		// mutate, and a Hydrate that writes through its input would make the
		// returned slice redundant — every caller would silently get the
		// rows whether or not it used the result.
		props := make(map[string]any, len(d.Props)+1)
		maps.Copy(props, d.Props)
		props["rows"] = rows
		d.Props = props
		kept = append(kept, d)
	}
	return kept, firstErr
}
