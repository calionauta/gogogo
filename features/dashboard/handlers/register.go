// The dashboard slice mounts itself through the recipe seam.
//
// Todo was converted because its handler is already built by the root for its
// own reasons; the dashboard is the opposite shape (this file is the only
// place its constructor is named), so it converts directly: the root learns
// the unit only through the generated registration.
package handlers

import (
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the dashboard handler and hands its declaration to the seam.
// The generated registration in internal/server calls it once at boot, from
// the entry `Capability.Register` names.
func Register(reg *recipe.Registry, d register.Deps) {
	reg.Add(NewHandler(d.Cfg, d.Auth, d.Queries, d.Slots).Recipe())
}
