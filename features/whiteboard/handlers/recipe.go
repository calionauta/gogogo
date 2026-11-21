// The whiteboard unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/prompts"
	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the whiteboard unit declares itself under.
const RecipeID = "whiteboard"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface whiteboard claims. It declares NO table: boards and
// document snapshots are created by the core migration ledger, so claiming them
// would be a declaration the core does not honour; the schema split is where the
// owned, prefixed migration moves them.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"
		doc  = "/api/whiteboard/{docID}"

		paramDocID = "docID"
		paramBody  = "body"
		helpDocID  = "The board's UUID."
	)
	return recipe.Declaration{
		// Boards are SHARED (see internal/db/queries/collab.sql): the list
		// page shows every board, whoever drew it. So "Total" counts them
		// all — the global spelling, no owner binding — and "Mine" counts
		// the ones this account owns. Counting only your own rows would
		// make the dashboard disagree with the list beside it.
		Metrics: []recipe.Metric{
			{Label: "Total", Query: "SELECT COUNT(*) FROM boards", Signal: "boardsTotal", OwnerBinds: recipe.WithoutOwner()},
			{Label: "Mine", Query: "SELECT COUNT(*) FROM boards WHERE owner = ?", Signal: "boardsMine"},
		},
		Panel: &recipe.PanelSpec{
			Component: "boards_list",
			Prompt:    prompts.Text("panels/boards-list"),
			Writable:  []string{"scope"},
		},
		Routes: []recipe.Route{
			{Method: get, Pattern: "/whiteboard"},
			{Method: get, Pattern: "/whiteboard/new"},
			{Method: get, Pattern: "/whiteboard/{docID}"},
			{Method: get, Pattern: "/api/whiteboards/fragment"},
			{Method: post, Pattern: "/api/whiteboards"},
			{Method: get, Pattern: doc + "/stream"},
			{Method: get, Pattern: doc + "/snapshot"},
			{Method: post, Pattern: doc + "/update"},
			{Method: post, Pattern: doc + "/presence"},
			{Method: post, Pattern: doc + "/delete"},
		},
		// Drawing is a real headless write: `update` forwards the same op
		// document the browser's offline queue replays, so an agent draws by
		// naming the op, not by pretending to be a pointer. Delete is the
		// owner/admin write the page already exposes.
		Actions: []recipe.InvokeAction{
			{
				Verb:        "snapshot",
				Description: "Read the board's current shape list as JSON.",
				Route:       recipe.Route{Method: get, Pattern: doc + "/snapshot"},
				PathParams:  []string{paramDocID},
				ParamHelp:   map[string]string{paramDocID: helpDocID},
			},
			{
				Verb:        "create",
				Description: "Create an empty board owned by the caller and return its id and page path.",
				Route:       recipe.Route{Method: post, Pattern: "/api/whiteboards"},
				// The page link is the same capability for a human; without it the
				// GET would be a create path no agent can reach.
				BrowserPath: "/whiteboard/new",
			},
			{
				Verb: "update",
				Description: "Apply one canvas op (add, move, clear, …) to a board the " +
					"caller may edit; the reply is the new shape list or a 409 with the current one.",
				Route:      recipe.Route{Method: post, Pattern: doc + "/update"},
				Params:     []string{paramBody},
				PathParams: []string{paramDocID},
				Query:      []string{"clientID"},
				RawBody:    paramBody,
				ParamHelp: map[string]string{
					paramDocID: helpDocID,
					"clientID": "The acting tab id, so the op is attributed and other tabs see who drew.",
					paramBody:  "The ShapeOp JSON document: op, baseVersion and shape, exactly as the browser sends it.",
				},
			},
			{
				Verb:        "delete",
				Description: "Delete a board; the caller must own it or be an admin. Irreversible.",
				Route:       recipe.Route{Method: post, Pattern: doc + "/delete"},
				PathParams:  []string{paramDocID},
				ParamHelp:   map[string]string{paramDocID: helpDocID},
			},
		},
		// Presence is a cursor broadcast: it writes no board state and answers
		// only a peer patch, so it is declared rather than dressed up as a verb.
		Omitted: []recipe.OmittedRoute{{
			Route:  recipe.Route{Method: post, Pattern: doc + "/presence"},
			Reason: "ephemeral cursor/peer broadcast; it persists nothing and answers a UI patch",
		}},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
