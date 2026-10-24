// SCOPE:feature — the jev-compatible Chat router (see config/chat_router.go).
// Moved from the deleted Ask slice: the classifier picks which component
// leads a Chat answer. To remove: delete this file + the router argument in
// internal/server/boot_helpers.go + config/chat_router.go + go mod tidy
// (drops github.com/mattn/go-jev when features/review is gone too).
package handlers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	jev "github.com/mattn/go-jev"

	"github.com/calionauta/gogogo/config"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/observability"
)

// routerQuestion is the one question the classifier answers. It names
// the decision, not a rubric: every candidate label carries the same
// weight, and the model picks the component that fits what was asked.
const routerQuestion = "component"

// routerInstructions asks for the shape, never the content. The router
// cannot fill props (it never sees the catalog's schemas), so a prompt
// that invited it to would invite invented fields that fail closed at
// parse time.
const routerInstructions = "Which single component should lead the answer to this request? " +
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
// chooses the component Chat leads with. Endpoint, model, key and timeout
// ride the shared JEV_* settings the review judge uses — one classifier
// endpoint for the whole app.
type jevRouter struct {
	client    jevClient
	threshold float64
}

// NewRouter builds the Chat router, or nil when the operator has not
// opted in. A nil router is the default and the compatibility guarantee:
// no client is constructed and the prompt stays byte for byte, so an
// existing install is untouched by this feature.
func NewRouter(cfg *config.Config) comp.Router {
	if cfg == nil || !cfg.ChatRouter.Enabled {
		return nil
	}
	return newJevRouter(jev.NewClient(
		jev.WithURL(jev.Endpoint(cfg.Review.APIURL)),
		jev.WithModel(cfg.Review.Model),
		jev.WithAPIKey(cfg.Review.APIKey),
		jev.WithTimeout(cfg.Review.Timeout),
	), cfg.ChatRouter.ConfidenceThreshold)
}

// newJevRouter is the test seam: NewRouter supplies the real client and
// the config threshold, tests supply a stub and their own threshold.
func newJevRouter(client jevClient, threshold float64) *jevRouter {
	return &jevRouter{client: client, threshold: threshold}
}

// Choose implements comp.Router for the default (Chat) vocabulary. Kept so the
// interface's legacy 1-arg form stays honest; ChooseFor is what the leg calls
// so a pick is always confined to the surfaces the leg advertises.
func (r *jevRouter) Choose(ctx context.Context, request string) (string, error) {
	return r.ChooseFor(ctx, request, comp.DefaultSurfacesChat)
}

// ChooseFor asks the classifier which component should lead, offering exactly
// the names the leg advertises. Every answer the classifier CAN give is either
// a confident, in-surface name or a fallback: a low-confidence pick, a label
// the leg does not advertise, an empty choice. Each of those returns ("", nil)
// — the leg keeps working with its whole vocabulary — and only a transport
// failure or a response that does not answer the question at all is an error.
func (r *jevRouter) ChooseFor(ctx context.Context, request string, surfaces []comp.Surface) (string, error) {
	candidates := comp.CatalogComponentsFor(surfaces)
	if len(candidates) == 0 || r.client == nil {
		return "", nil
	}
	criteria := make(map[string]any, len(candidates))
	for _, name := range candidates {
		criteria[name] = nil
	}
	resp, err := r.client.Evaluate(ctx, request, map[string]jev.Question{
		routerQuestion: {
			Type:         "choice",
			Instructions: routerInstructions,
			Criteria:     criteria,
		},
	})
	if err != nil {
		return "", fmt.Errorf("genui: chat router: %w", err)
	}
	if resp == nil {
		return "", errors.New("genui: chat router: empty response")
	}
	answer := resp.Answers[routerQuestion]
	if answer == nil {
		return "", errors.New("genui: chat router: no answer to the component question")
	}
	name := answer.Choice
	if answer.Confidence < r.threshold {
		observability.Info(ctx, "genui: chat router below threshold — offering the whole catalog",
			append(observability.WithComponent("genui", "feature"),
				"chosen", name, "confidence", answer.Confidence, "threshold", r.threshold)...)
		return "", nil
	}
	if !slices.Contains(candidates, name) {
		observability.Info(ctx, "genui: chat router chose a name outside the catalog",
			append(observability.WithComponent("genui", "feature"),
				"chosen", name, "surfaces", comp.Names(surfaces))...)
		return "", nil
	}
	observability.Info(ctx, "genui: chat router chose the lead component",
		append(observability.WithComponent("genui", "feature"),
			"chosen", name, "confidence", answer.Confidence)...)
	return name, nil
}
