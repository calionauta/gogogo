// The landing slice mounts itself through the recipe seam.
//
// Register moves the knowledge of landing's constructor into the unit; the
// composition root learns the unit only through the generated registration.
// The 404 handler stays at the root (it is a Router setting, not a route),
// resolved from the seam registry like every other Recipe() consumer.
package handlers

import (
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the landing handler and hands its declaration to the seam.
func Register(reg *recipe.Registry, d register.Deps) {
	reg.Add(NewHandler(d.Cfg, d.Auth).Recipe())
}
