// SCOPE:plugin — the optional decision router in front of the Ask prompt.
// Split from responders.go for the file-size gate: responders.go holds the
// model legs, this file holds the seam that picks what the prompt leads
// with and the one place that pick reaches the model.
package components

import (
	"context"

	"github.com/calionauta/gogogo/internal/observability"
)

// Router decides which catalog component an Ask answer should lead with.
// The production implementation asks a jev-compatible classifier
// (features/genui/handlers); tests pass a stub. It reports a NAME only —
// never props, never the answer — because the router answers "which
// shape", and the model still composes the content for the request it
// was given.
//
// An empty name is not a failure: it means "no confident pick", and the
// whole catalog is offered exactly as if no router existed. Only a
// transport error is an error, and AskPrompt logs it and falls back too —
// a router that is on but unusable must never fail a request.
type Router interface {
	Choose(ctx context.Context, request string) (string, error)
}

// AskPrompt composes the Ask prompt for one request. This is the single
// place the router's pick can reach the model, which is what makes "the
// router changes nothing when off" provable instead of asserted: with no
// router the result is byte-identical to
// PromptForCatalog()+"\nUser request: "+request, and TestAskPromptRouterOff
// pins that.
func AskPrompt(ctx context.Context, router Router, request string) string {
	if router == nil {
		return promptForRequest(PromptForCatalog(), request)
	}
	name, err := router.Choose(ctx, request)
	if err != nil {
		// The tab already shows its spinner, so the fallback IS the feature
		// working. Logged, because a router that always fails is a
		// misconfiguration an operator has to be able to see (a silent
		// fallback looks exactly like an unused router).
		observability.Warn(ctx, "genui: ask router unavailable — offering the whole catalog",
			append(observability.WithComponent("genui", "feature"), "err", err.Error())...)
		return promptForRequest(PromptForCatalog(), request)
	}
	return promptForRequest(PromptForCatalogLead(name), request)
}

// promptForRequest is the one shape every Ask leg shares, so the router
// path and the plain path cannot drift apart.
func promptForRequest(instructions, request string) string {
	return instructions + "\nUser request: " + request
}
