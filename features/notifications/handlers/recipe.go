// The inbox declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the inbox declares itself under. It is also the
// shell's module id, so the rail and the route name the same destination.
const RecipeID = "notifications"

// Recipe returns the declaration the composition root mounts through the seam.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// signalInboxUnread is the inbox unread count signal, shared by the badge
// patch (chi.go) and the declared dashboard metric (recipe.go).
const signalInboxUnread = "inboxUnread"

func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
	)
	return recipe.Declaration{
		Metrics: []recipe.Metric{
			{
				Label:  "Unread",
				Query:  "SELECT COUNT(*) FROM notifications WHERE owner = ? AND read_at IS NULL",
				Signal: signalInboxUnread,
			},
			{Label: "Total", Query: "SELECT COUNT(*) FROM notifications WHERE owner = ?", Signal: "totalInbox"},
		},
		Panel: &recipe.PanelSpec{
			Component: "inbox_list",
			Prompt:    "inbox_list {scope: unread|all} whenever the request involves the user's reminders or inbox",
			Writable:  []string{"scope"},
		},
		Routes: []recipe.Route{
			{Method: get, Pattern: "/notifications"},
			{Method: get, Pattern: "/api/notifications/stream"},
			{Method: post, Pattern: "/api/notifications/remind"},
			{Method: post, Pattern: "/api/notifications/read-all"},
			{Method: post, Pattern: "/api/notifications/{id}/read"},
			{Method: post, Pattern: "/api/notifications/{id}/unread"},
			{Method: get, Pattern: "/api/notifications/badge"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
