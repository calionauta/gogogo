// The inbox declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/prompts"
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
			Prompt:    prompts.Text("panels/inbox-list"),
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
		// The inbox is fully scriptable: scheduling a reminder and moving a row
		// between read and unread are the same owner-scoped writes the page
		// performs, so an agent never needs the browser for them.
		Actions: []recipe.InvokeAction{
			{
				Verb:        "remind",
				Description: "Schedule one reminder for the calling session and return the refreshed inbox patch.",
				Route:       recipe.Route{Method: post, Pattern: "/api/notifications/remind"},
				Params:      []string{"text", "delay", "channels"},
				ParamHelp: map[string]string{
					"text":     "The reminder body.",
					"delay":    "When it fires: 1m, 1h or 24h.",
					"channels": "One offered channel name; omit for in-app only.",
				},
			},
			{
				Verb:        "read_all",
				Description: "Mark every in-app notification of the calling owner as read.",
				Route:       recipe.Route{Method: post, Pattern: "/api/notifications/read-all"},
			},
			{
				Verb:        "read",
				Description: "Mark one of the caller's notifications as read.",
				Route:       recipe.Route{Method: post, Pattern: "/api/notifications/{id}/read"},
				PathParams:  []string{"id"},
				ParamHelp:   map[string]string{"id": "The notification's UUID."},
			},
			{
				Verb:        "unread",
				Description: "Return one of the caller's notifications to the unread pile.",
				Route:       recipe.Route{Method: post, Pattern: "/api/notifications/{id}/unread"},
				PathParams:  []string{"id"},
				ParamHelp:   map[string]string{"id": "The notification's UUID."},
			},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
