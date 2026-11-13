// The coffee recipe's boot entry point.
//
// `Register` is the one function the generated registration calls. It builds the
// handler, hands its provider to the seam, contributes its sidebar entry and
// offers its chat panel — and it mounts nothing, so a route collision stays the
// registry's to report rather than a panic from inside a recipe.
package handlers

import (
	"context"

	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register wires the recipe into the composed tree.
func Register(reg *recipe.Registry, d register.Deps) {
	h := NewHandler(d.Auth, d.Queries, d.Cfg)
	reg.Add(h.Recipe())

	// The panel is the recipe's chat surface. It renders the page's own list, so
	// the answer cannot drift from /coffee. The root asserts it to the panel
	// interface and installs the registry once, with every other recipe's.
	if d.Panels != nil {
		*d.Panels = append(*d.Panels, h.Panel())
	}

	// The nav entry is the recipe's only page contribution: a destination, not a
	// fragment, because the shell lays out its own rail and an extension has no
	// business drawing inside it.
	if d.Slots == nil {
		return
	}
	err := d.Slots.Add(recipe.Contribution{
		Entries: map[recipe.SlotID][]recipe.Entry{
			recipe.SlotNavSidebar: {{Label: "Coffee", Href: "/coffee"}},
		},
	})
	if err != nil {
		// A build that offers no nav slot cannot show the destination. The routes
		// still work, so a boot-time warning is the honest report: failing here
		// would take the whole tree down for a missing menu entry. Boot has no
		// request context, so the empty one keeps the log on the app's logger.
		observability.Warn(context.Background(), "coffee: sidebar entry refused",
			append(observability.WithComponent("coffee", "handler"), "error", err)...)
	}
}
