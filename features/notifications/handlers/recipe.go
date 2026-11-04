// SCOPE:feature — the inbox declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the inbox declares itself under. It is also the
// shell's module id, so the rail and the route name the same destination.
const RecipeID = "notifications"

// Recipe returns the declaration the composition root mounts through the seam.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: "/notifications"},
			{Method: get, Pattern: "/api/notifications/stream"},
			{Method: post, Pattern: "/api/notifications/remind"},
			{Method: post, Pattern: "/api/notifications/read-all"},
			{Method: post, Pattern: "/api/notifications/{id}/read"},
			{Method: get, Pattern: "/api/notifications/badge"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
