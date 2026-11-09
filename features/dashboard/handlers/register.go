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
	h := NewHandler(d.Cfg, d.Auth, d.Queries, d.Slots)
	// Chat/credits counts ride their refs (filled by those Registers,
	// which run before this one in the generated order). Nil-safe: a
	// trimmed recipe simply leaves its card off.
	if d.ChatRef != nil && d.ChatRef.Count != nil {
		h.SetChatRef(d.ChatRef.Count)
	}
	if d.CreditsRef != nil && d.CreditsRef.Balance != nil {
		h.SetCreditsRef(d.CreditsRef.Balance)
	}
	reg.Add(h.Recipe())
}
