// SCOPE:plugin — the pomodoro recipe declares its surface to the seam.
package handlers

import (
	"io/fs"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/features/pomodoro"
	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe's id, matching .recipe.yml and the receipt.
const RecipeID = "pomodoro"

// Recipe returns the declaration the composition root mounts through the seam.
// It lives beside the routes it describes so the two cannot drift without one
// edit: install compares this against the receipt, and the receipt against the
// route patterns the handler registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface this recipe claims. Tables carry the id prefix and
// are created only by this recipe's own migration; a core table declared here
// would be refused at install.
func (p recipeProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{
		Tables: []string{"ext_pomodoro_sessions"},
		Routes: []recipe.Route{
			{Method: "GET", Pattern: "/pomodoro"},
			{Method: "POST", Pattern: "/api/pomodoro/sessions"},
		},
		Slots: []string{string(recipe.SlotNavSidebar)},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }

// Migrations is the optional schema half of the seam: the recipe's own DDL,
// embedded so the binary carries it and applied through the core ledger under
// this recipe's id.
func (p recipeProvider) Migrations() fs.FS { return pomodoro.Migrations }
