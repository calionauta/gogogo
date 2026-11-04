// SCOPE:feature — the config view declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the config-view unit declares itself under.
const RecipeID = "config-view"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface config-view claims. It has no table and no job:
// the page reads the core users and audit rows, so claiming either would make
// Validate refuse it — which is the point of declaring.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: "/config"},
			{Method: post, Pattern: "/config/users/role"},
			{Method: post, Pattern: "/config/users/revoke"},
			{Method: get, Pattern: "/config/audit.csv"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
