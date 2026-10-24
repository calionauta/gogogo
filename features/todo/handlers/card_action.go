// SCOPE:feature — answering a model-authored card's action, in place.
//
// Split from mutations.go for the file-size gate, and along a real seam: that
// file owns the HTTP mutations a person triggers on the todo page, this one owns
// what a CATALOG CARD's button does — the slot it names, the identifiers it
// carries, and the in-place success/failure cards. The two share only the
// handler's dependencies.
//
// The whole file exists because a card is not the todo page: it can be rendered
// on Chat, which hosts neither #todo-list nor any other region this
// feature owns. Aiming a response there matched nothing, so the row was created
// and the page in front of the user did not change ("I clicked and nothing
// happened"). Everything here is about answering where the click happened.
package handlers

import (
	"net/http"
	"strings"

	"github.com/starfederation/datastar-go/datastar"

	ic "github.com/calionauta/gogogo/internal/components"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/observability"
)

// formTargetField is the hidden input a catalog action sends with its own card
// id. Namespaced with the same leading underscore as the other server-owned
// props so it is recognizable as "the framework told you this, not the model".
const formTargetField = "_target"

// formTarget reads the caller's own slot id, or "" when there is none.
//
// VALIDATED, not trusted: this value becomes a Datastar selector, so an
// unconstrained one is a way to aim a patch at any element on the page (or, with
// a crafted string, at something the responder did not intend). The same
// character rule the instance ids are minted under is applied here, which means
// only ids this app could have generated are accepted — a hand-written or
// crafted value is ignored and the response falls back to the list patch.
func formTarget(r *http.Request) string {
	target := strings.TrimSpace(r.FormValue(formTargetField))
	if !validTargetID(target) {
		return ""
	}
	return target
}

// validIdemKey bounds a client-supplied deduplication token.
//
// Shape, not format: a UUID is 36 chars, a `plan-<ts>-<rand>` fallback a little
// more, and both are [A-Za-z0-9_-]. Requiring a strict UUID would reject the
// fallback older clients send (the card action's own
// `crypto.randomUUID`-or-timestamp expression), and rejecting a legitimate
// client over a deduplication token is a worse failure than the size bound it
// would buy.
func validIdemKey(key string) bool {
	if key == "" || len(key) > 128 {
		return false
	}
	for _, r := range key {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			return false
		}
	}
	return true
}

// validTargetID mirrors the instance-id rule (letters, digits, - and _; not
// starting with a digit; bounded length). Spelled here rather than imported
// from features/genui/components because a trim removes that package and this
// handler must keep compiling — and the rule is a security boundary, so it
// wants to be visible next to the code that trusts it.
func validTargetID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			return false
		}
		if i == 0 && r >= '0' && r <= '9' {
			return false
		}
	}
	return true
}

// answerCardFailure sends the in-place failure card for a rejected action.
//
// It exists because a card's errors had nowhere to land: `@post` from a
// model-authored component renders nothing for an error response BODY, so a
// validation rejection (an empty title, an unreadable request) looked to the
// user like a dead button — the exact complaint this whole change set began
// with, in its second form. The card is patched with ActionFailed at the slot
// the user actually clicked.
//
// Status is 200: the SSE stream opened successfully and carries the outcome.
// A 4xx would make Datastar treat it as a transport error and drop the patch,
// which is the opposite of what this is for. The failure is signalled in the
// payload and logged.
//
//nolint:contextcheck // SSE emission ends in datastar-go sinks (no ctx param)
func (h *Handler) answerCardFailure(
	w http.ResponseWriter, r *http.Request, target, publicMsg string,
) {
	sse := datastar.NewSSE(w, r)
	if err := dshelpers.RenderAndPatch(sse,
		ic.ActionResult(target, false, publicMsg, "", ""),
		datastar.WithSelector("#"+target)); err != nil {
		// Nothing left to tell the user: the failure card IS the error report,
		// and its own write failed. Log it so the cause is not silent.
		observability.Error(r.Context(), "todo: action failure patch failed",
			append(observability.WithComponent("todo", "feature"), "error", err)...)
	}
}

// answerCardAction replaces the clicked card with its outcome, in place.
//
// Returns the write error instead of failing here: the response is already an
// open SSE stream at this point, so the caller owns the failure report (and it
// needs the ResponseWriter to send it). No ctx parameter for that reason — the
// only work left is a write to an already-open stream.
func (h *Handler) answerCardAction(
	sse *datastar.ServerSentEventGenerator, target, title string,
) error {
	return dshelpers.RenderAndPatch(sse,
		ic.ActionResult(target, true, "Added to your todos: "+title, "Open Todos", "/todo"),
		datastar.WithSelector("#"+target))
}
