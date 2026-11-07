// The credits unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the credits unit declares itself under.
const RecipeID = "credits"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface credits claims. It has no table and no job: the
// ledger is the unit's own separate SQLite file (OpenTursoLedger), never a
// table in the core migration ledger, and claiming a table the core owns would
// make Validate refuse it — which is the point of declaring.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		post = "POST"
		byok = "/api/byok/*"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: "GET", Pattern: "/api/credits"},
			{Method: post, Pattern: "/api/ai/request"},
			{Method: post, Pattern: "/api/credits/checkout"},
			{Method: post, Pattern: "/api/credits/stripe-webhook"},
			{Method: "GET", Pattern: byok},
			{Method: post, Pattern: byok},
			{Method: "PUT", Pattern: byok},
			{Method: "PATCH", Pattern: byok},
			{Method: "DELETE", Pattern: byok},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
