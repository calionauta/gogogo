// The config-view slice mounts itself through the recipe seam.
package handlers

import (
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the config handler, wires its slot surface, and hands
// its declaration to the seam. The handler was previously constructed in
// the composition root and fed SetSlots afterwards — the setter moves
// here so the root never names the constructor.
func Register(reg *recipe.Registry, d register.Deps) {
	h := NewHandler(d.Cfg, d.Auth, d.Queries)
	h.SetSlots(d.Slots)
	h.SetProviders(reg)
	reg.Add(h.Recipe())
}
