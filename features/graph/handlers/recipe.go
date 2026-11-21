// The graph slice declares its surface: read-only panels, no write.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the id this unit declares itself under.
const RecipeID = "graph"

// Recipe returns the declaration the composition root mounts.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration claims the page and its read endpoints. Every route is a GET: the
// graph observes the system, it never changes it, so there is no mutation to
// make callable or to omit.
func (p recipeProvider) Declaration() recipe.Declaration {
	const get = "GET"
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: "/graph"},
			{Method: get, Pattern: "/api/graph/panorama"},
			{Method: get, Pattern: "/api/graph/dagnats"},
			{Method: get, Pattern: "/api/graph/presence"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
