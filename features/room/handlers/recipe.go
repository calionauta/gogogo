// SCOPE:feature — the room unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the room unit declares itself under.
const RecipeID = "room"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
//
// A nil handler is the engine-disabled boot (GOAKT_ENABLED=false): the unit
// has no code to mount, so it returns no provider and the seam registers
// nothing. Returning a declaration there would claim nine routes the composed
// tree does not serve — the gate reports exactly that, which is what keeps
// this absence honest.
func (h *Handler) Recipe() recipe.Provider {
	if h == nil {
		return nil
	}
	return recipeProvider{h: h}
}

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface room claims. It has no table and no job: the
// roster is rebuilt from the actor engine on boot, and the demo owns no rows
// of its own, so claiming one would make Validate refuse it.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: "/room"},
			{Method: get, Pattern: "/room/roster"},
			{Method: get, Pattern: "/room/stream"},
			{Method: post, Pattern: "/room/heartbeat"},
			{Method: post, Pattern: "/room/join"},
			{Method: post, Pattern: "/room/acquire"},
			{Method: post, Pattern: "/room/release"},
			{Method: post, Pattern: "/room/leave"},
			{Method: post, Pattern: "/room/crash"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
