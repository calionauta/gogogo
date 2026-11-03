// SCOPE:feature — the review unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the review unit declares itself under.
const RecipeID = "review"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface review claims. It has no table and no job: the
// page reads genkit and holds no rows of its own, and claiming one it does not
// own would make Validate refuse it — which is the point of declaring.
func (p recipeProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: "GET", Pattern: "/review"},
			{Method: "POST", Pattern: "/api/review/run"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
