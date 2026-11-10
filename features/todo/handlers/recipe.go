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

// Route patterns, one spelling each: a rename that missed one would split the
// declared surface from the mounted one, which the composition gate refuses.
const (
	methodGet  = "GET"
	methodPost = "POST"

	todosAPI      = "/api/todos"
	todoPage      = "/todo"
	todoToggle    = "/api/todos/{id}/toggle"
	todoDelete    = "/api/todos/{id}/delete"
	todoClearDone = "/api/todos/completed/delete"
	todoSuggest   = "/api/todos/suggest"
	todoSuggestUI = "/api/todos/suggest-simulated"
	todoRetryDemo = "/api/todos/retry-demo"
	loginPath     = "/login"
	signupPath    = "/signup"
)

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
	return recipe.Declaration{
		// The counts are the rows the PAGE shows: the todo list reads the
		// visible set (your own rows plus the ones shared with everyone),
		// so a dashboard that counted only your own rows would disagree
		// with the list beside it. The table is the todo unit's OWN
		// (`ext_todo_todos`) — the core `todos` seed is dropped at boot
		// once this unit's migration set lands, so a query naming it
		// fails and the card disappears.
		Metrics: []recipe.Metric{
			{
				Label:  "Open",
				Query:  "SELECT COUNT(*) FROM ext_todo_todos WHERE (owner = ? OR visibility = 'public') AND completed = 0",
				Signal: "openTodos",
			},
			{
				Label:  "Done",
				Query:  "SELECT COUNT(*) FROM ext_todo_todos WHERE (owner = ? OR visibility = 'public') AND completed = 1",
				Signal: "doneTodos",
			},
			{
				Label:  "Total",
				Query:  "SELECT COUNT(*) FROM ext_todo_todos WHERE owner = ? OR visibility = 'public'",
				Signal: "totalTodos",
			},
			{
				// Shared counts the rows everybody sees, so it does not
				// bind the owner (the global spelling).
				Label:      "Shared",
				Query:      "SELECT COUNT(*) FROM ext_todo_todos WHERE visibility = 'public'",
				Signal:     "sharedTodos",
				OwnerBinds: recipe.WithoutOwner(),
			},
		},
		Panel: &recipe.PanelSpec{
			Component: "todo_list",
			Prompt:    "todo_list {filter: open|done|all} whenever the request involves the user's OWN todos or tasks",
			Writable:  []string{"filter"},
		},
		Tables:  []string{"ext_todo_todos"},
		Actions: todoActions(),
		Omitted: todoOmitted(),
		Routes:  todoRoutes(),
	}
}

// todoActions is the headless half. `toggle` and `delete` take the repository's
// `{id}` (a UUID); clear_completed is the one bulk mutation an agent can run
// without naming a row. The demo/LLM routes are not actions — see todoOmitted.
func todoActions() []recipe.InvokeAction {
	return []recipe.InvokeAction{
		{
			Verb:        "create",
			Description: "Create a todo owned by the calling session.",
			Route:       recipe.Route{Method: methodPost, Pattern: todosAPI},
			Params:      []string{SearchTitleField, "idem_key", "visibility"},
			ParamHelp: map[string]string{
				SearchTitleField: "The todo text.",
				"idem_key":       "Client-generated key that makes a retry idempotent.",
				"visibility":     "private or public.",
			},
		},
		{
			Verb:        "toggle",
			Description: "Toggle a todo's completed state; the caller must own it.",
			Route:       recipe.Route{Method: methodPost, Pattern: todoToggle},
			PathParams:  []string{"id"},
			ParamHelp:   map[string]string{"id": "The todo's UUID."},
		},
		{
			Verb:        "delete",
			Description: "Delete one of the caller's own todos; irreversible.",
			Route:       recipe.Route{Method: methodPost, Pattern: todoDelete},
			PathParams:  []string{"id"},
			ParamHelp:   map[string]string{"id": "The todo's UUID."},
		},
		{
			Verb:        "clear_completed",
			Description: "Delete every completed todo owned by the calling session.",
			Route:       recipe.Route{Method: methodPost, Pattern: todoClearDone},
		},
	}
}

// todoOmitted is the deliberate half of the headless surface. Each reason
// names why the route cannot complete synchronously or has no product outcome,
// so `recipe check` can tell a decision from a forgotten action.
func todoOmitted() []recipe.OmittedRoute {
	return []recipe.OmittedRoute{
		{
			Route:  recipe.Route{Method: methodPost, Pattern: todoSuggest},
			Reason: "asynchronous LLM job; the answer arrives through the todo stream, never synchronously",
		},
		{
			Route:  recipe.Route{Method: methodPost, Pattern: todoSuggestUI},
			Reason: "UI demo of the suggest stepper; the real LLM path is the suggest job",
		},
		{
			Route:  recipe.Route{Method: methodPost, Pattern: todoRetryDemo},
			Reason: "UI demo of the queue/retry stepper; it has no product outcome to invoke",
		},
	}
}

func todoRoutes() []recipe.Route {
	return []recipe.Route{
		{Method: methodGet, Pattern: todosAPI},
		{Method: methodGet, Pattern: "/api/todos/search"},
		{Method: methodGet, Pattern: "/api/todos/fragment"},
		{Method: methodPost, Pattern: todosAPI},
		{Method: methodPost, Pattern: todoToggle},
		{Method: methodPost, Pattern: todoClearDone},
		{Method: methodPost, Pattern: todoDelete},
		{Method: methodGet, Pattern: "/api/todos/{id}/confirm-delete"},
		{Method: methodGet, Pattern: "/api/todos/stream"},
		{Method: methodGet, Pattern: "/api/todos/events"},
		{Method: methodPost, Pattern: todoSuggest},
		{Method: methodPost, Pattern: todoSuggestUI},
		{Method: methodPost, Pattern: todoRetryDemo},
		{Method: methodGet, Pattern: loginPath},
		{Method: methodPost, Pattern: loginPath},
		{Method: methodGet, Pattern: signupPath},
		{Method: methodPost, Pattern: signupPath},
		{Method: methodPost, Pattern: "/logout"},
		{Method: methodGet, Pattern: todoPage},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }

// Migrations is the optional schema half of the seam: the unit's own DDL,
// embedded so the binary carries it and applied through the core ledger under
// this unit's id. The composition root re-runs this set on every boot through
// the same Registry an installed recipe uses, so an in-tree unit and an
// installed recipe own storage through one path.
func (p recipeProvider) Migrations() fs.FS { return todo.Migrations }
