// SCOPE:feature — the account slice mounts itself through the recipe seam.
//
// Why this file exists: the composition root used to build this handler inline.
// That line is one of the reasons a unit could not be removed — the root knew
// the constructor, its dependency order and its nil checks. Register moves the
// knowledge into the unit, and the root keeps one generated entry instead.
package handlers

import (
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the account handler and hands its declaration to the seam.
// The generated registration in internal/server calls it once at boot, from
// the entry `Capability.Register` names.
func Register(reg *recipe.Registry, d register.Deps) {
	reg.Add(NewHandler(d.Cfg, d.Auth, d.Queries).Recipe())
}
