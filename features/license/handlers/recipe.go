// The license gate declares its surface: one page, one activation write.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the id this unit declares itself under.
const RecipeID = "license"

// Recipe returns the declaration the composition root mounts. A nil handler is
// a disabled gate (LICENSE_MODE=none) and declares nothing.
func (h *Handler) Recipe() recipe.Provider {
	if h == nil {
		return nil
	}
	return recipeProvider{h: h}
}

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration claims the page and the activation write. Activation is an
// omission, not a verb: it takes an entitlement token the OPERATOR minted, and
// an agent that could install its own entitlement would grant itself the right
// to run the product.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
		path = "/activate"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: path},
			{Method: post, Pattern: path},
		},
		Omitted: []recipe.OmittedRoute{
			{
				Route: recipe.Route{Method: post, Pattern: path},
				Reason: "activation installs an entitlement the operator minted: an agent " +
					"that could present its own token would grant itself the right to run " +
					"the product, which is a licensing decision, not a task.",
			},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
