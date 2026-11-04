// SCOPE:feature — the account page declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the account unit declares itself under.
const RecipeID = "account"

// Recipe returns the declaration the composition root mounts through the
// recipe seam.
//
// The unit mounts later than its neighbours only because the root built it
// after composing the tree. Building it from the same dependency bundle the
// root already has removes that ordering exception, so the declaration lives
// beside its routes like every other converted unit.
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
			{Method: get, Pattern: "/account"},
			{Method: post, Pattern: "/account/password"},
			{Method: post, Pattern: "/account/verify"},
			{Method: post, Pattern: "/account/totp/begin"},
			{Method: post, Pattern: "/account/totp/verify"},
			{Method: post, Pattern: "/account/totp/disable"},
			{Method: post, Pattern: "/account/totp/recovery"},
			{Method: post, Pattern: "/account/sessions/revoke"},
			{Method: post, Pattern: "/account/sessions/revoke-all"},
			{Method: post, Pattern: "/account/cloud"},
			{Method: post, Pattern: "/account/cloud/disconnect"},
			{Method: post, Pattern: "/account/device-token"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
