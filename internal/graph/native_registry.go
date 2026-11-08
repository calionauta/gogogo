// Native provider registry: how a recipe opts into the gogpu/ui window
// without the window importing the recipe.
//
// A web recipe already installs routes/tables/slots through .recipes/*.yml.
// This registry is the native half of that contract: a package that wants
// a native twin registers a Provider (id + builder) from init(), cmd/gui
// renders every registered provider below the todo list, and a recipe
// without a provider simply does not appear natively. Registration is
// order-stable (sort by id) so frames never flicker between builds.
package graph

import (
	"sort"

	"github.com/gogpu/ui/widget"
)

// Provider is one native surface. Build runs on the UI thread and reads a
// fresh Document each frame, so it never fetches frames itself.
type Provider struct {
	ID    string
	Build func(Document) widget.Widget
}

var providers []Provider

// Register adds a native provider. Registration order does not matter:
// Providers() sorts, so two recipes cannot race for position. Duplicate
// ids panic at init — a silent overwrite would hide a recipe collision
// until the window opened.
func Register(p Provider) {
	for _, existing := range providers {
		if existing.ID == p.ID {
			panic("graph native: duplicate provider " + p.ID)
		}
	}
	providers = append(providers, p)
}

// Providers returns every registered native surface, sorted by id.
func Providers() []Provider {
	out := make([]Provider, len(providers))
	copy(out, providers)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
