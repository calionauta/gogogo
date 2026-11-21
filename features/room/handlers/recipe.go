// The room unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the room unit declares itself under.
const RecipeID = "room"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
//
// A nil handler is the engine-disabled boot (GOAKT_ENABLED=false): the unit
// has no code to mount, so it returns no provider and the seam registers
// nothing. Returning a declaration there would claim nine routes the composed
// tree does not serve — the gate reports exactly that, which is what keeps
// this absence honest.
func (h *Handler) Recipe() recipe.Provider {
	if h == nil {
		return nil
	}
	return recipeProvider{h: h}
}

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface room claims. It has no table and no job: the
// roster is rebuilt from the actor engine on boot, and the demo owns no rows
// of its own, so claiming one would make Validate refuse it.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: "/room"},
			{Method: get, Pattern: "/room/roster"},
			{Method: get, Pattern: "/room/stream"},
			{Method: post, Pattern: "/room/heartbeat"},
			{Method: post, Pattern: "/room/join"},
			{Method: post, Pattern: "/room/acquire"},
			{Method: post, Pattern: "/room/release"},
			{Method: post, Pattern: "/room/leave"},
			{Method: post, Pattern: "/room/crash"},
		},
		// The room's whole vocabulary is a click: join, leave, take the pen,
		// give it back. Every one of them reads the tab's clientID from the
		// query (never the form) and answers the roster as JSON unless the
		// caller asked for a Datastar patch — so the same endpoints serve an
		// agent and the browser without a second surface.
		Actions: []recipe.InvokeAction{
			roomAction("join", "Put this client back in the room roster after a leave.", "/room/join"),
			roomAction("leave", "Take this client out of the room roster.", "/room/leave"),
			roomAction("acquire", "Take the pen for this client.", "/room/acquire"),
			roomAction("release", "Give the pen back for this client.", "/room/release"),
		},
		Omitted: []recipe.OmittedRoute{
			{
				Route:  recipe.Route{Method: post, Pattern: "/room/heartbeat"},
				Reason: "ambient liveness ping the live stream already replaces; it changes no product state",
			},
			{
				Route:  recipe.Route{Method: post, Pattern: "/room/crash"},
				Reason: "demo failure injection: it restarts the room actor to watch recovery, not a task a caller performs",
			},
		},
	}
}

// roomAction is the one shape all four roster verbs share: a POST that reads
// the tab id from the query and answers the roster. Declaring them through one
// helper keeps the query name from drifting per route.
func roomAction(verb, description, pattern string) recipe.InvokeAction {
	return recipe.InvokeAction{
		Verb:        verb,
		Description: description,
		Route:       recipe.Route{Method: "POST", Pattern: pattern},
		Query:       []string{"clientID"},
		ParamHelp:   map[string]string{"clientID": "The tab id whose roster entry to move."},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
