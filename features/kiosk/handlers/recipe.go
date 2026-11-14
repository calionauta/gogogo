// The kiosk recipe declares its surface to the seam.
package handlers

import (
	"io/fs"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/features/kiosk"
	"github.com/calionauta/gogogo/internal/prompts"
	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe's id, matching .recipe.yml and the receipt.
const RecipeID = "kiosk"

const (
	methodGet  = "GET"
	methodPost = "POST"
)

// Recipe returns the declaration the composition root mounts through the seam.
// It lives beside the routes it describes so the two cannot drift without one
// edit: install compares this against the receipt, and the receipt against the
// route patterns the handler registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface this recipe claims. Tables carry the id prefix and
// are created only by this recipe's own migration; a core table declared here
// would be refused at install.
//
// The outcome is the pair of counts, not a stream of events: `corrected` is a
// column on the order, so the ratio is read from rows the recipe already owns.
func (p recipeProvider) Declaration() recipe.Declaration {
	return recipe.Declaration{
		Tables: []string{"ext_kiosk_orders"},
		Routes: []recipe.Route{
			{Method: methodGet, Pattern: "/kiosk"},
			{Method: methodPost, Pattern: "/api/kiosk/preview"},
			{Method: methodPost, Pattern: "/api/kiosk/confirm"},
		},
		Slots: []string{string(recipe.SlotNavSidebar)},
		Actions: []recipe.InvokeAction{{
			Verb:        "confirm",
			Description: "Confirm one order for the calling owner, parsed from the posted text.",
			Route:       recipe.Route{Method: methodPost, Pattern: "/api/kiosk/confirm"},
			Params:      []string{"text", "previewed"},
			ParamHelp: map[string]string{
				"text":      "The spoken or typed order, in the recipe's own menu vocabulary.",
				"previewed": "The preview serialization the caller was shown; empty when nothing was previewed.",
			},
		}},
		// Preview writes nothing and answers with a patch, so a headless caller
		// would receive a signal frame and no result. Declared rather than
		// forgotten, which is the difference the omission check reports.
		Omitted: []recipe.OmittedRoute{{
			Route:  recipe.Route{Method: methodPost, Pattern: "/api/kiosk/preview"},
			Reason: "Demo-only parse that writes nothing and answers with a UI patch; it never completes headless.",
		}},
		Metrics: []recipe.Metric{{
			Label: "Orders confirmed",
			Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ?",
		}},
		Outcomes: []recipe.Outcome{{
			ID:          "order_without_repair",
			Job:         "Receive the order you asked for without fixing a misread order",
			Beneficiary: "user",
			Success: recipe.Metric{
				Label: "Orders without repair",
				Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ? AND corrected = 0",
			},
			Total: recipe.Metric{
				Label: "Orders confirmed",
				Query: "SELECT COUNT(*) FROM ext_kiosk_orders WHERE owner = ?",
			},
			CounterSignals: []string{"corrected"},
			Window:         "session",
		}},
		Panel: &recipe.PanelSpec{
			Component: "kiosk_orders",
			Prompt:    prompts.Text("panels/kiosk-orders"),
			Writable:  []string{"scope"},
			Values:    []string{scopeRecent, scopeAll},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }

// Migrations is the optional schema half of the seam: the recipe's own DDL,
// embedded so the binary carries it and applied through the core ledger under
// this recipe's id.
func (p recipeProvider) Migrations() fs.FS { return kiosk.Migrations }
