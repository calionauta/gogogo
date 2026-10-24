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
