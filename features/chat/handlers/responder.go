// SCOPE:feature — HTTP chat LLM bridge (model-leg constructor).
package handlers

import (
	"context"

	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/llm"

	"github.com/calionauta/gogogo/features/chat/store"
)

// ResponderFromLLM bridges the generative model leg to the chat Responder:
// identical method shape, distinct named types so neither feature imports
// the other's handler. Lives here (not in internal/server) so trimming the
// genui bundle deletes it with features/chat — the composition root keeps
// only line-drops, never a block strip.
//
// router and surfaces ride the Chat configuration (nil = off/defaults).
func ResponderFromLLM(real, simulated *llm.Client, router comp.Router, surfaces []comp.Surface) store.Responder {
	return responderAdapter{inner: comp.NewResponder(real, simulated, router, surfaces)}
}

// responderAdapter adapts comp.Responder to the chat store seam.
type responderAdapter struct {
	inner comp.Responder
}

func (ad responderAdapter) Respond(ctx context.Context, prompt string) (string, error) {
	return ad.inner.Respond(ctx, prompt)
}

// WithOwner implements store.OwnerContext by forwarding to the generative leg, which
// owns the data-digest implementation.
func (ad responderAdapter) WithOwner(owner string) store.Responder {
	if scoped, ok := ad.inner.(comp.OwnerContext); ok {
		return responderAdapter{inner: scoped.WithOwner(owner)}
	}
	return ad
}

// responderFor builds the responder for one job's surface set. The model leg
// (clients + router + default) comes from SetModelLeg at boot; the surfaces
// come from the tab's toggles on the job. Tests inject a stub responder and
// never call SetModelLeg, so a missing leg keeps the stub — canned answers
// ignore surfaces, which is exactly what a stub is for.
func (h *Handler) responderFor(surfaces []comp.Surface) store.Responder {
	if h.realLLM == nil && h.simLLM == nil {
		return h.responder
	}
	return ResponderFromLLM(h.realLLM, h.simLLM, h.router, surfaces)
}

// proseOffFallback is the honest line when the model answered only with
// bands the tab turned off: nothing rendered, and the fix is one tap away
// (the toggles sit above the composer), not a silent hole.
func proseOffFallback() []comp.Directive {
	return []comp.Directive{{
		Component: "text_note",
		Props: map[string]any{"text": "That answer needed a component group that's " +
			"currently off — turn one back on above, or ask again in plain words."},
	}}
}
