// SCOPE:feature — the landing page declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the landing unit declares itself under.
const RecipeID = "landing"

// Recipe returns the declaration the composition root mounts through the
// recipe seam.
//
// One route, and that is the whole split this unit needed: GET / is a page a
// recipe serves, while "answer every path nobody registered" is the shell's
// not-found policy and stays core. Keeping the two together is what made the
// unit look un-convertible.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

func (p recipeProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{Routes: []recipe.Route{{Method: "GET", Pattern: "/"}}}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
