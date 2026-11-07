// The pomodoro recipe's boot entry point.
//
// `Register` is the one function the generated registration calls. It builds the
// handler, hands its provider to the seam and contributes its sidebar entry — and
// it mounts nothing, so a route collision stays the registry's to report rather
// than a panic from inside a recipe.
package handlers

import (
	"log/slog"

	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register wires the recipe into the composed tree.
func Register(reg *recipe.Registry, d register.Deps) {
	reg.Add(NewHandler(d.Auth, d.Queries, d.Cfg).Recipe())

	// The nav entry is the recipe's only page contribution: a destination, not a
	// fragment, because the shell lays out its own rail and an extension has no
	// business drawing inside it.
	if d.Slots == nil {
		return
	}
	err := d.Slots.Add(recipe.Contribution{
		Entries: map[recipe.SlotID][]recipe.Entry{
			recipe.SlotNavSidebar: {{Label: "Pomodoro", Href: "/pomodoro"}},
		},
	})
	if err != nil {
		// A build that offers no nav slot cannot show the destination. The routes
		// still work, so a boot-time warning is the honest report: failing here
		// would take the whole tree down for a missing menu entry. Boot has no
		// request context, which is the one place slog is the right logger.
		slog.Warn("pomodoro: sidebar entry refused", "error", err)
	}
}
