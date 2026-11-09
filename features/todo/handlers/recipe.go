// The todo unit declares its surface to the recipe seam.
package handlers

import (
	"io/fs"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/features/todo"
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

// Declaration is the surface todo claims. It now declares its table:
// `ext_todo_todos` is created only by this unit's own migration
// (features/todo/migrations, applied through the core ledger under the "todo"
// namespace), and the declaration <-> DDL gate in internal/server checks both
// directions — this is the first in-tree table-owning conversion, proving an
// in-tree unit can own DDL the same way an installed recipe does. The legacy
// core `todos` seed is dropped after the set applies (see
// internal/server/recipe_surface.go): this migration carries the rows into
// the owned table, every sqlc query reads only the owned table, and the
// legacy append-only copy does not linger.
//
// The auth pages ride this router (they are the demo's entry surface), so they
// are declared with the rest: the claim table attributes routes by who mounts
// them, and a route that ships without a declaration is the failure the gate
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
		Metrics: []recipe.Metric{
			{Label: "Open", Query: "SELECT COUNT(*) FROM todos WHERE owner = ? AND completed = 0", Signal: "openTodos"},
			{Label: "Done", Query: "SELECT COUNT(*) FROM todos WHERE owner = ? AND completed = 1", Signal: "doneTodos"},
			{Label: "Total", Query: "SELECT COUNT(*) FROM todos WHERE owner = ?", Signal: "totalTodos"},
			{
				Label:  "Shared",
				Query:  "SELECT COUNT(*) FROM todos WHERE owner = ? AND visibility = 'public'",
				Signal: "sharedTodos",
			},
		},
		Panel: &recipe.PanelSpec{
			Component: "todo_list",
			Prompt:    "todo_list {filter: open|done|all} whenever the request involves the user's OWN todos or tasks",
			Writable:  []string{"filter"},
		},
		Tables: []string{"ext_todo_todos"},
		Actions: []recipe.InvokeAction{
			{
				Verb: "create", Route: recipe.Route{Method: post, Pattern: todosAPI},
				Params: []string{"title", "idem_key", "visibility"},
			},
		},
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

// Migrations is the optional schema half of the seam: the unit's own DDL,
// embedded so the binary carries it and applied through the core ledger under
// this unit's id. The composition root re-runs this set on every boot through
// the same Registry an installed recipe uses, so an in-tree unit and an
// installed recipe own storage through one path.
func (p recipeProvider) Migrations() fs.FS { return todo.Migrations }
