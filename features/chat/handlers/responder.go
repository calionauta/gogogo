// SCOPE:feature — HTTP chat LLM bridge (model-leg constructor).
package handlers

import (
	"context"

	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/llm"

	"github.com/calionauta/gogogo/features/chat/store"
)

// ResponderFromLLM bridges the genui model leg to the chat Responder:
// identical method shape, distinct named types so neither feature imports
// the other's handler. Lives here (not in internal/server) so trimming the
// genui bundle deletes it with features/chat — the composition root keeps
// only line-drops, never a block strip.
//
// router and surfaces are the same values Ask gets (nil = off/defaults): both
// legs share one surface vocabulary and one decision about which component
// should lead, so the only difference left between them is the interaction
// shape.
func ResponderFromLLM(real, simulated *llm.Client, router comp.Router, surfaces []comp.Surface) store.Responder {
	return responderAdapter{inner: comp.NewChatResponder(real, simulated, router, surfaces)}
}

// responderAdapter adapts comp.Responder to the chat store seam.
type responderAdapter struct {
	inner comp.Responder
}

func (ad responderAdapter) Respond(ctx context.Context, prompt string) (string, error) {
	return ad.inner.Respond(ctx, prompt)
}

// WithOwner implements store.OwnerContext by forwarding to the genui leg, which
// owns the data-digest implementation.
func (ad responderAdapter) WithOwner(owner string) store.Responder {
	if scoped, ok := ad.inner.(comp.OwnerContext); ok {
		return responderAdapter{inner: scoped.WithOwner(owner)}
	}
	return ad
}
