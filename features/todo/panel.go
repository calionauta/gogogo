// SCOPE:feature — the todo data shapes the app's own panel exposes.
//
// These used to live in features/genui/components/catalog.go, which made the
// generative-UI plugin the owner of a feature's data shapes (and gave it a
// filter list it had no way to validate). The panel seam is provider-based, so
// the shapes belong to the feature that owns the page: the catalog knows names
// and props in the abstract, never what a todo is.
//
// The MODEL-FACING panel name is not declared here: the provider that owns it
// returns it from `Component()` (features/todo/handlers/genui_panel.go), and a
// second constant would be a second spelling of one name. These are the DATA
// shapes only.
//
// To remove: delete with features/todo/.
package todo

import "slices"

// ListFilters is the closed set the todo_list panel's `filter` prop accepts, in
// prompt order. Named, not numbered: the store is asked for the named slice, and
// the panel's prompt line is built from this list, so the advertised filters
// cannot drift from the accepted ones.
var ListFilters = []string{"open", "done", "all"}

// FilterValid reports whether a filter is in the closed set. The owner is the
// feature, so a panel and the page validate against the same list.
func FilterValid(filter string) bool {
	return slices.Contains(ListFilters, filter)
}

// PanelRows is the panel's rendered view of one row: the app's truth, filled
// server-side by the panel provider and never supplied by the model.
type PanelRows struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}
