// The notes unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the notes unit declares itself under.
const RecipeID = "notes"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface notes claims. It declares NO table on purpose: the
// `notes` rows are created by the core migration ledger, so claiming them would
// be a declaration the core does not honour. The table moves in with the schema
// split, when a prefixed recipe migration owns it.
const propScope = "scope"

func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
		doc  = "/api/notes/{docID}"
	)
	return recipe.Declaration{
		// Notes are SHARED (see internal/db/queries/collab.sql): the list
		// page shows every note, whoever wrote it. So "Total" counts them
		// all — the global spelling, no owner binding — and "Mine" counts
		// the ones this account wrote. Counting only your own rows would
		// make the dashboard disagree with the list beside it.
		Metrics: []recipe.Metric{
			{Label: "Total", Query: "SELECT COUNT(*) FROM notes", Signal: "notesTotal", OwnerBinds: recipe.WithoutOwner()},
			{Label: "Mine", Query: "SELECT COUNT(*) FROM notes WHERE owner = ?", Signal: "notesMine"},
		},
		Panel: &recipe.PanelSpec{
			Component: "notes_list",
			Prompt:    "notes_list {scope: all|mine} whenever the request involves the user's shared notes",
			Writable:  []string{propScope},
		},
		Routes: []recipe.Route{
			{Method: get, Pattern: "/notes"},
			{Method: get, Pattern: "/notes/new"},
			{Method: get, Pattern: "/notes/{docID}"},
			{Method: get, Pattern: "/api/notes/fragment"},
			{Method: post, Pattern: doc + "/delete"},
			{Method: get, Pattern: doc + "/stream"},
			{Method: post, Pattern: doc + "/op"},
			{Method: post, Pattern: doc + "/typing"},
			{Method: post, Pattern: doc + "/caret"},
			{Method: post, Pattern: doc + "/leave"},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
