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
			Writable:  []string{propLimit},
		},
		Routes: []recipe.Route{
			{Method: get, Pattern: "/chat"},
			{Method: get, Pattern: "/api/chat/messages"},
			{Method: post, Pattern: "/api/chat/ask"},
			{Method: post, Pattern: "/api/chat/clear"},
			{Method: get, Pattern: "/api/chat/stream"},
		},
		// The demo has no privileged write: asking is enqueuing a job and
		// clearing is deleting the caller's own history. `ask` describes the
		// async contract honestly — the answer arrives on the chat stream, not
		// in the invoke reply — so an agent is not surprised by the ack.
		Actions: []recipe.InvokeAction{
			{
				Verb:        "history",
				Description: "Read the calling session's chat history as JSON, oldest first; limit keeps the newest N.",
				Route:       recipe.Route{Method: get, Pattern: "/api/chat/messages"},
				Query:       []string{"limit"},
				ParamHelp: map[string]string{
					"limit": "Optional positive count of newest messages to return; omit for the whole thread.",
				},
			},
			{
				Verb: "ask",
				Description: "Ask the assistant one question; the job is queued and the " +
					"answer arrives on the chat stream, not in this reply.",
				Route:  recipe.Route{Method: post, Pattern: "/api/chat/ask"},
				Params: []string{"prompt", "surfaces"},
				ParamHelp: map[string]string{
					"prompt":   "The question to ask.",
					"surfaces": "Optional comma-separated genui surfaces (catalog, custom, native).",
				},
			},
			{
				Verb:        "clear",
				Description: "Delete the calling session's whole chat history.",
				Route:       recipe.Route{Method: post, Pattern: "/api/chat/clear"},
			},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
