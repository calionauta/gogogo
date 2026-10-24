// SCOPE:plugin — the optional decision router in front of the Chat prompt.
// Split from responders.go for the file-size gate: responders.go holds the
// model leg, this file holds the seam that picks what the prompt leads
// with and the one place that pick reaches the model.
package components

import (
	"context"

	"github.com/calionauta/gogogo/internal/observability"
)

// Router decides which component an answer should lead with. The production
// implementation is the chat slice's jev-compatible classifier
// (features/chat/handlers); tests pass a stub. It reports a NAME only —
// never props, never the answer — because the router answers "which shape",
// and the model still composes the content for the request it was given.
//
// An empty name is not a failure: it means "no confident pick", and the
// whole catalog is offered exactly as if no router existed. Only a
// transport error is an error, and ConversationPrompt logs it and falls
// back too — a router that is on but unusable must never fail a request.
type Router interface {
	// Choose is the legacy 1-arg form: the Chat default vocabulary.
	Choose(ctx context.Context, request string) (string, error)
	// ChooseFor is the surface-aware form. The Chat leg calls this, so a
	// pick is always confined to the vocabulary it advertises.
	ChooseFor(ctx context.Context, request string, surfaces []Surface) (string, error)
}

// ConversationPrompt composes the Chat prompt for one request. This is the
// single place the router's pick can reach the model, which is what makes
// "the router changes nothing when off" provable instead of asserted.
func ConversationPrompt(ctx context.Context, router Router, request string, surfaces []Surface) string {
	surfaces = NormalizeSurfaces(surfaces)
	base := PromptForSurfaces(surfaces)
	if name, ok := routerPick(ctx, router, surfaces, request); ok {
		base = PromptForCatalogLeadSurfaces(name, surfaces)
	}
	return promptForRequest(base, request)
}

// routerPick asks the router for a lead component and reports whether the
// answer is usable for THIS surface set. Every miss — no router, a transport
// error, a low-confidence pick, a name not advertised — is (…, false), never
// an error: the tab is already showing a spinner, so the fallback IS the
// feature working. The result is checked against the announced set, so a
// router can never point the leg at a surface it lacks.
func routerPick(ctx context.Context, router Router, surfaces []Surface, request string) (string, bool) {
	if router == nil {
		return "", false
	}
	name, err := router.ChooseFor(ctx, request, surfaces)
	if err != nil {
		// Logged, because a router that always fails is a misconfiguration an
		// operator has to be able to see (a silent fallback looks exactly like
		// an unused router).
		observability.Warn(ctx, "genui: chat router unavailable — offering the whole catalog",
			append(observability.WithComponent("genui", "feature"), "err", err.Error())...)
		return "", false
	}
	if name == "" || !advertisedAs(name, surfaces) || !Registered(name) {
		return "", false
	}
	return name, true
}

// promptForRequest is the one shape the leg uses, so the router path and
// the plain path cannot drift apart.
//
// NOTE: the envelope stays prompt-embedded on purpose. Wiring the provider's
// native structured output (goai's ResponseFormat) would need a JSON schema
// plus a per-provider capability matrix — a feature, not a prompt tweak — and
// the prompt-embedded envelope is what the tolerant parser is designed around
// (models do answer with fences and stray prose, and both parse paths handle
// it). Revisit as its own change if the repair rate says shapes are the
// bottleneck.
func promptForRequest(instructions, request string) string {
	return instructions + "\nUser request: " + request
}
