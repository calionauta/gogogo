// SCOPE:feature — the todo panel's filter vocabulary.
//
// This used to live in features/genui/components/catalog.go, which made the
// generative-UI plugin the owner of a feature's data shapes (and gave it a
// filter list it had no way to validate). The panel seam is provider-based, so
// the vocabulary belongs to the feature that owns the page: the catalog knows
// names and props in the abstract, never what a todo is.
//
// The MODEL-FACING panel name is not declared here: the provider that owns it
// returns it from `Component()` (features/todo/handlers/genui_panel.go), and a
// second constant would be a second spelling of one name. The panel's ROW shape
// is todo.Todo — deliberately not a "panel row" type: the panel renders the
// same TodoItem as the page, and a reduced shape is how the first panel lost
// the shared badge, the author, the age and the delete gate.
//
// To remove: delete with features/todo/.
package todo

import (
	"slices"
	"strings"
)

// FilterOpen, FilterDone and FilterAll are the canonical members of the
// closed set, named as constants so the normalizer, the list and the
// consumers cannot drift apart by spelling. They are also what goconst wants:
// the same three strings appear in the alias tables and in ListFilters.
const (
	FilterOpen = "open"
	FilterDone = "done"
	FilterAll  = "all"
)

// defaultFilter is the slice the model means when it omits `filter` — or
// writes it outside `props`, which the parser correctly drops: a todo question
// is about what is still to do. The Chat composer's own example is "what are
// my open todos?", and an omitted filter answered it with completed rows mixed
// in ("open todos" reported as all). The panel names the slice it opened, so a
// wider request the model forgot to spell costs one glance, not a wrong list.
// Before any default existed an omitted filter failed validation, the panel
// was dropped by Hydrate, and the bubble came back empty — the model had
// answered correctly and the user saw nothing.
const defaultFilter = FilterOpen

// NormalizeFilter maps what a model actually writes onto the closed set.
//
// Models capitalize enums ("ALL"), pad them, and occasionally answer with a
// synonym or a plural; the prompt names the enum, so a near-miss is a
// presentation difference, not a different request. Anything unrecognizable
// still fails closed (FilterValid is the arbiter) — this normalizes spelling,
// it does not widen the set. Lived bug: `filter: "ALL"` was rejected, so the
// panel was dropped and the answer showed nothing.
func NormalizeFilter(filter string) string {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "":
		return defaultFilter
	case FilterOpen, "todo", "todos", "pending", "active", "incomplete", "unfinished", "not done":
		return FilterOpen
	case FilterDone, "complete", "completed", "finished", "closed":
		return FilterDone
	case FilterAll, "everything", "any", "*":
		return FilterAll
	default:
		return filter
	}
}

// ListFilters is the closed set the todo_list panel's `filter` prop accepts, in
// prompt order. Named, not numbered: the store is asked for the named slice, and
// the panel's prompt line is built from this list, so the advertised filters
// cannot drift from the accepted ones.
var ListFilters = []string{FilterOpen, FilterDone, FilterAll}

// FilterValid reports whether a filter is in the closed set. The owner is the
// feature, so a panel and the page validate against the same list.
func FilterValid(filter string) bool {
	return slices.Contains(ListFilters, filter)
}
