// The config view declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the config-view unit declares itself under.
const RecipeID = "config-view"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface config-view claims. It has no table and no job:
// the page reads the core users and audit rows, so claiming either would make
// Validate refuse it — which is the point of declaring.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: "/config"},
			{Method: post, Pattern: "/config/users/role"},
			{Method: post, Pattern: "/config/users/revoke"},
			{Method: post, Pattern: "/config/recipes/toggle"},
			{Method: get, Pattern: "/config/audit.csv"},
		},
		// The operator surface is deliberately not an agent's: each route below
		// changes who may act on this box. An agent that could grant itself a
		// role, or switch a capability on, would be authoring its own
		// permissions — the one write the recipe model must not expose. They
		// are declared, not merely absent, so a reviewer can tell a decision
		// from a forgotten route.
		Omitted: []recipe.OmittedRoute{
			{
				Route:  recipe.Route{Method: post, Pattern: "/config/users/role"},
				Reason: "privilege: changing a user's role is how an actor gains authority; an agent must not grant its own",
			},
			{
				Route: recipe.Route{Method: post, Pattern: "/config/users/revoke"},
				Reason: "privilege: revoking another account's sessions is an " +
					"operator containment action, not a task a caller performs on itself",
			},
			{
				Route: recipe.Route{Method: post, Pattern: "/config/recipes/toggle"},
				Reason: "privilege: switching a capability on or off changes what " +
					"the whole product mounts; it belongs to the operator, through " +
					"`gogogo recipe enable|disable`",
			},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
