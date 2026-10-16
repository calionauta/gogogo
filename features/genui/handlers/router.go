// SCOPE:plugin — the jev-compatible Ask router (see config/ask_router.go).
// To remove: delete this file + the router argument in internal/server/chi.go
// + config/ask_router.go + go mod tidy (drops github.com/mattn/go-jev when
// features/review is gone too).
package handlers

import (
	"context"
	"errors"
	"fmt"

	jev "github.com/mattn/go-jev"

	"github.com/calionauta/gogogo/config"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/observability"
)

// askRouterQuestion is the one question the classifier answers. It names
// the decision, not a rubric: every candidate label carries the same
// weight, and the model picks the component that fits what was asked.
const askRouterQuestion = "component"

// askRouterInstructions asks for the shape, never the content. The router
// cannot fill props (it never sees the catalog's schemas), so a prompt
// that invited it to would invite invented fields that fail closed at
// parse time.
const askRouterInstructions = "Which single component should lead the answer to this request? " +
	"Choose the one that best fits what the user is asking for. " +
	"Choose text_note only when the request wants plain prose with no structure."

// jevClient is the classifier leg. *jev.Client satisfies it; tests stub it
// (the same 1-method seam features/review/handlers uses for its judge).
type jevClient interface {
	Evaluate(ctx context.Context, state, questions any) (*jev.Response, error)
}

// Compile-time check that the mattn client satisfies the seam.
var _ jevClient = (*jev.Client)(nil)

// jevRouter is the production comp.Router: a jev-compatible classifier
// chooses the component Ask leads with. Endpoint, model, key and timeout
// ride the shared JEV_* settings the review judge uses — one classifier
// endpoint for the whole app.
type jevRouter struct {
	client    jevClient
	threshold float64
}

// NewAskRouter builds the Ask router, or nil when the operator has not
// opted in. A nil router is the default and the compatibility guarantee:
// no client is constructed and AskPrompt returns the catalog-only prompt
// byte for byte, so an existing install is untouched by this feature.
func NewAskRouter(cfg *config.Config) comp.Router {
	if cfg == nil || !cfg.AskRouter.Enabled {
		return nil
	}
	return newJevRouter(jev.NewClient(
		jev.WithURL(jev.Endpoint(cfg.Review.APIURL)),
		jev.WithModel(cfg.Review.Model),
		jev.WithAPIKey(cfg.Review.APIKey),
		jev.WithTimeout(cfg.Review.Timeout),
	), cfg.AskRouter.ConfidenceThreshold)
}

// newJevRouter is the test seam: NewAskRouter supplies the real client and
// the config threshold, tests supply a stub and their own threshold.
func newJevRouter(client jevClient, threshold float64) *jevRouter {
	return &jevRouter{client: client, threshold: threshold}
}

// Choose asks the classifier which catalog component should lead. Every
// answer the classifier CAN give is either a confident, in-catalog name or
// a fallback: a low-confidence pick, a label the catalog does not have (or
// html_block, which only chat may emit), an empty choice. Each of those
// returns ("", nil) — Ask keeps working with the whole catalog — and only a
// transport failure or a response that does not answer the question at all
// is an error, for AskPrompt to log.
func (r *jevRouter) Choose(ctx context.Context, request string) (string, error) {
	candidates := comp.CatalogComponents()
	if len(candidates) == 0 || r.client == nil {
		return "", nil
	}
	criteria := make(map[string]any, len(candidates))
	for _, name := range candidates {
		criteria[name] = nil
	}
	// No ctx wrapper beyond the caller's: the worker runs this off the
	// request path, and jev.Client already carries the JEV_TIMEOUT bound.
	resp, err := r.client.Evaluate(ctx, request, map[string]jev.Question{
		askRouterQuestion: {
			Type:         "choice",
			Instructions: askRouterInstructions,
			Criteria:     criteria,
		},
	})
	if err != nil {
		return "", fmt.Errorf("genui: ask router: %w", err)
	}
	if resp == nil {
		return "", errors.New("genui: ask router: empty response")
	}
	answer := resp.Answers[askRouterQuestion]
	if answer == nil {
		return "", errors.New("genui: ask router: no answer to the component question")
	}
	name := answer.Choice
	if answer.Confidence < r.threshold {
		observability.Info(ctx, "genui: ask router below threshold — offering the whole catalog",
			append(observability.WithComponent("genui", "feature"),
				"chosen", name, "confidence", answer.Confidence, "threshold", r.threshold)...)
		return "", nil
	}
	if !comp.Registered(name) || name == "html_block" {
		// A name outside the catalog would either render nothing or reach
		// chat's affordance, so it is a miss, not a pick.
		observability.Info(ctx, "genui: ask router chose a name outside the catalog",
			append(observability.WithComponent("genui", "feature"), "chosen", name)...)
		return "", nil
	}
	observability.Info(ctx, "genui: ask router chose the lead component",
		append(observability.WithComponent("genui", "feature"),
			"chosen", name, "confidence", answer.Confidence)...)
	return name, nil
}
