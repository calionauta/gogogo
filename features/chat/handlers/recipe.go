// The chat unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/prompts"
	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the chat unit declares itself under.
const RecipeID = "chat"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot
// drift without one edit: the gate in internal/server compares this against
// the routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface chat claims. It declares no table: history
// persists through the in-memory MessageStore, and catalog surfaces ride
// the genui seam.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
	)
	return recipe.Declaration{
		// Chat history lives in the in-memory MessageStore (no COUNT
		// query exists) — Messages/Human ride the ChatRef hook, not a
		// declared metric. The panel IS declared (chat_history slice).
		Panel: &recipe.PanelSpec{
			Component: "chat_history",
			Prompt:    prompts.Text("panels/chat-history"),
			Writable:  []string{"limit"},
		},
		Routes: []recipe.Route{
			{Method: get, Pattern: "/chat"},
			{Method: post, Pattern: "/api/chat/ask"},
			{Method: post, Pattern: "/api/chat/clear"},
			{Method: get, Pattern: "/api/chat/stream"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
