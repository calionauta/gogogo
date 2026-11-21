// The onboarding slice declares its surface: one job-start write, no page.
package onboarding

import (
	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/recipe"
)

// onboardingRecipeID is the id this unit declares itself under. It is spelled
// locally rather than as the conventional RecipeID because this package is the
// engine (no handlers/ directory, which is where the convention puts the
// constant); TestOnboardingRecipeIDMatchesTheUnit pins the value against the
// capability row so the two cannot drift.
const onboardingRecipeID = "onboarding"

// Recipe returns the declaration the composition root mounts. A nil handler is
// a disabled engine (DAGNATS_ENABLED=false) and declares nothing.
func (h *Handler) Recipe() recipe.Provider {
	if h == nil {
		return nil
	}
	return recipeProvider{h: h}
}

type recipeProvider struct{ h *Handler }

func (p recipeProvider) ID() string { return onboardingRecipeID }

// Declaration claims the durable-workflow start as a real verb: kicking off the
// onboarding run is something a caller genuinely does, and the answer arrives on
// the stream rather than in the reply.
func (p recipeProvider) Declaration() recipe.Declaration {
	const post = "POST"
	return recipe.Declaration{
		Routes: []recipe.Route{
			{Method: post, Pattern: "/api/onboarding/start"},
		},
		Actions: []recipe.InvokeAction{
			{
				Verb: "start",
				Description: "Start the durable onboarding workflow. The run's progress " +
					"arrives on the todo stream; this reply only confirms the start.",
				Route: recipe.Route{Method: post, Pattern: "/api/onboarding/start"},
				Query: []string{"clientID"},
				ParamHelp: map[string]string{
					"clientID": "The acting tab id, so streamed progress reaches the tab that started the run.",
				},
				BrowserPath: "/todo",
			},
		},
	}
}

func (p recipeProvider) Routes() chi.Router { return p.h.Routes() }
