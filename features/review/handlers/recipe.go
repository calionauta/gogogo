// The review unit declares its surface to the recipe seam.
package handlers

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// RecipeID is the recipe id the review unit declares itself under.
const RecipeID = "review"

// Recipe returns the declaration the composition root mounts through the
// recipe seam. It lives beside the routes it describes so the two cannot drift
// without one edit: the gate in internal/server compares this against the
// routes the handler actually registers.
func (h *Handler) Recipe() recipe.Provider { return recipeProvider{h: h} }

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return RecipeID }

// Declaration is the surface review claims. It has no table and no job: the
// page reads genkit and holds no rows of its own, and claiming one it does not
// own would make Validate refuse it — which is the point of declaring.
func (p recipeProvider) Declaration() recipe.Declaration {
	const (
		get  = "GET"
		post = "POST"

		paramTab     = "tab"
		paramContent = "content"
	)
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: get, Pattern: "/review"},
			{Method: post, Pattern: "/api/review/run"},
			{Method: post, Pattern: "/api/review/verdict"},
		},
		Actions: []recipe.InvokeAction{
			{
				Verb: "verdict",
				Description: "Run the review loop on one document and return the final " +
					"decision as JSON (decision, rationale, confidence, rounds, proposal).",
				Route:  recipe.Route{Method: "POST", Pattern: "/api/review/verdict"},
				Params: []string{paramTab, paramContent},
				ParamHelp: map[string]string{
					paramTab:     "Which review lens to run.",
					paramContent: "The document to review.",
				},
			},
			{
				Verb: "run",
				Description: "Run the review loop on one document with live progress; the " +
					"verdict arrives as streamed patch frames when it completes.",
				Route:  recipe.Route{Method: "POST", Pattern: "/api/review/run"},
				Params: []string{paramTab, paramContent},
				ParamHelp: map[string]string{
					paramTab:     "Which review lens to run.",
					paramContent: "The document to review.",
				},
			},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
