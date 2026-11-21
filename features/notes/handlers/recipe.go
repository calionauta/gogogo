// The notes unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/prompts"
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

		paramDocID = "docID"
		paramBody  = "body"
		helpDocID  = "The note's UUID."
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
			Prompt:    prompts.Text("panels/notes-list"),
			Writable:  []string{propScope},
		},
		Routes: []recipe.Route{
			{Method: get, Pattern: "/notes"},
			{Method: get, Pattern: "/notes/new"},
			{Method: get, Pattern: "/notes/{docID}"},
			{Method: get, Pattern: "/api/notes/fragment"},
			{Method: get, Pattern: "/api/notes/{docID}"},
			{Method: post, Pattern: "/api/notes"},
			{Method: post, Pattern: doc + "/delete"},
			{Method: get, Pattern: doc + "/stream"},
			{Method: post, Pattern: doc + "/op"},
			{Method: post, Pattern: doc + "/typing"},
			{Method: post, Pattern: doc + "/caret"},
			{Method: post, Pattern: doc + "/leave"},
		},
		// Editing is the product: `op` carries the same ordered text batch the
		// browser sends (base revision + ops + optional caret), so an agent
		// writes text through the one path that already resolves conflicts.
		Actions: []recipe.InvokeAction{
			{
				Verb:        "create",
				Description: "Create an empty note owned by the caller and return its id and page path.",
				Route:       recipe.Route{Method: post, Pattern: "/api/notes"},
				// The page link is the same capability for a human; without it the
				// GET would be a create path no agent can reach.
				BrowserPath: "/notes/new",
			},
			{
				Verb:        "read",
				Description: "Read one note's merged text and current revision as JSON.",
				Route:       recipe.Route{Method: get, Pattern: "/api/notes/{docID}"},
				PathParams:  []string{paramDocID},
				ParamHelp:   map[string]string{"docID": "The note's UUID."},
			},
			{
				Verb: "edit",
				Description: "Apply one batch of text ops to a note the caller may edit; " +
					"a stale base returns 409 with the authoritative revision.",
				Route:      recipe.Route{Method: post, Pattern: doc + "/op"},
				Params:     []string{paramBody},
				PathParams: []string{paramDocID},
				Query:      []string{"clientID"},
				RawBody:    "body",
				ParamHelp: map[string]string{
					paramDocID: helpDocID,
					"clientID": "The acting tab id, so peers see who is editing and the caret is attributed.",
					paramBody:  "The op batch JSON: base (server revision), ops (ordered TextOp list) and an optional caret.",
				},
			},
			{
				Verb:        "delete",
				Description: "Delete a note; the caller must own it or be an admin. Irreversible.",
				Route:       recipe.Route{Method: post, Pattern: doc + "/delete"},
				PathParams:  []string{paramDocID},
				ParamHelp:   map[string]string{"docID": "The note's UUID."},
			},
		},
		// The presence three are ephemeral broadcasts: no snapshot, no history,
		// nothing a later reader can observe. Declared, not dressed up as verbs.
		Omitted: []recipe.OmittedRoute{
			{
				Route:  recipe.Route{Method: post, Pattern: doc + "/typing"},
				Reason: "ephemeral typing indicator; it persists nothing and only peers see it",
			},
			{
				Route:  recipe.Route{Method: post, Pattern: doc + "/caret"},
				Reason: "ephemeral caret position; a later reader cannot observe it",
			},
			{
				Route:  recipe.Route{Method: post, Pattern: doc + "/leave"},
				Reason: "presence teardown for a tab that is going away; there is no product outcome to invoke",
			},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
