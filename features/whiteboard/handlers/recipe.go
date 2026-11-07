// The whiteboard unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the whiteboard unit declares itself under.
const RecipeID = "whiteboard"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface whiteboard claims. It declares NO table: boards and
// document snapshots are created by the core migration ledger, so claiming them
// would be a declaration the core does not honour; the schema split is where the
// owned, prefixed migration moves them.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
		doc  = "/api/whiteboard/{docID}"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: "/whiteboard"},
			{Method: get, Pattern: "/whiteboard/new"},
			{Method: get, Pattern: "/whiteboard/{docID}"},
			{Method: get, Pattern: "/api/whiteboards/fragment"},
			{Method: get, Pattern: doc + "/stream"},
			{Method: get, Pattern: doc + "/snapshot"},
			{Method: post, Pattern: doc + "/update"},
			{Method: post, Pattern: doc + "/presence"},
			{Method: post, Pattern: doc + "/delete"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
