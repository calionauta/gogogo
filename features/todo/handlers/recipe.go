// SCOPE:feature — the todo unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the todo unit declares itself under.
const RecipeID = "todo"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface todo claims. It declares NO table on purpose:
// `todos` is created by the core migration ledger and is in isCoreTable, so
// claiming it would make Validate refuse the declaration and Mount panic
// before boot. The recipe model's answer is the schema split (a prefixed,
// recipe-owned migration), and the table declaration moves here when that
// lands — not before, because a declaration the core does not honour is the
// drift class this seam exists to prevent.
//
// The auth pages ride this router (they are the demo's entry surface), so they
// are declared with the rest: the claim table attributes routes by who mounts
// them, and a route that ship without a declaration is the failure the gate
// reports.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
		// Repeated patterns, one spelling: goconst is right that a rename
		// that missed one of these would split the declared surface.
		todosAPI = "/api/todos"
		todoPage = "/todo"
		login    = "/login"
		signup   = "/signup"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: todosAPI},
			{Method: get, Pattern: "/api/todos/search"},
			{Method: get, Pattern: "/api/todos/fragment"},
			{Method: post, Pattern: todosAPI},
			{Method: post, Pattern: "/api/todos/{id}/toggle"},
			{Method: post, Pattern: "/api/todos/completed/delete"},
			{Method: post, Pattern: "/api/todos/{id}/delete"},
			{Method: get, Pattern: "/api/todos/{id}/confirm-delete"},
			{Method: get, Pattern: "/api/todos/stream"},
			{Method: get, Pattern: "/api/todos/events"},
			{Method: post, Pattern: "/api/todos/suggest"},
			{Method: post, Pattern: "/api/todos/suggest-simulated"},
			{Method: post, Pattern: "/api/todos/retry-demo"},
			{Method: get, Pattern: login},
			{Method: post, Pattern: login},
			{Method: get, Pattern: signup},
			{Method: post, Pattern: signup},
			{Method: post, Pattern: "/logout"},
			{Method: get, Pattern: todoPage},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
