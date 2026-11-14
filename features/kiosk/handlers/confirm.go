// The confirm half: the only write in the demo, and the only place the outcome
// is produced.
package handlers

import (
	"net/http"
	"strings"

	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/kiosk"
	"github.com/calionauta/gogogo/features/kiosk/components"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/observability"
)

// confirm re-parses the posted text and writes the order.
//
// The server parses again instead of trusting fields the page assembled: the
// text is the input, the order is the output, and a browser-supplied drink ID
// would be a value the client chose. `previewed` is the serialization the page
// was shown; the service compares it with the parsed order and records
// `corrected` when they differ.
func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.session(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	sse := sdk.NewSSE(w, r)
	// The SAME decision the preview made: a confirm must not reject an order the
	// page just showed as ready, so the engine decides here too.
	text := r.PostFormValue("text")
	parsed := h.engine.Settle(r.Context(), text, kiosk.Parse(text))
	if !parsed.Complete() {
		// Not a 400: the page asked, the answer is "not yet", and the same
		// signals that explain the preview explain this.
		signals := previewSignals(parsed)
		signals["kioskNotice"] = "Finish the order before confirming."
		if err := dshelpers.MergeSignals(sse, signals); err != nil {
			observability.Warn(r.Context(), "kiosk: confirm refusal signals failed",
				append(observability.WithComponent("kiosk", observability.LayerFeature), "error", err)...)
		}
		return
	}
	previewed := strings.TrimSpace(r.PostFormValue("previewed"))
	if err := h.orders.Confirm(r.Context(), sess.User.ID, parsed.Order, previewed); err != nil {
		observability.Error(r.Context(), "kiosk: confirm failed",
			append(observability.WithComponent("kiosk", observability.LayerFeature), "error", err)...)
		if serr := dshelpers.MergeSignals(sse, map[string]any{
			"kioskNotice": "The order could not be recorded.",
		}); serr != nil {
			observability.Warn(r.Context(), "kiosk: confirm error signals failed",
				append(observability.WithComponent("kiosk", observability.LayerFeature), "error", serr)...)
		}
		return
	}
	rows, err := h.orders.List(r.Context(), sess.User.ID, ordersPageLimit)
	if err != nil {
		observability.Error(r.Context(), "kiosk: list after confirm failed",
			append(observability.WithComponent("kiosk", observability.LayerFeature), "error", err)...)
		http.Error(w, "listing orders failed", http.StatusInternalServerError)
		return
	}
	// The list patch names its region by id AND selector: a patch without a
	// target is the failure mode the repo's Datastar rule exists to prevent.
	//nolint:contextcheck // Datastar-go sinks take no context (PatchElements writes
	// to the ResponseWriter); cancellation surfaces as a write error, which the
	// branch below reports. Same reasoning as features/todo/handlers/chi.go.
	if err := dshelpers.RenderAndPatch(sse, components.OrdersList(orderViews(rows)),
		sdk.WithSelectorID("kiosk-orders"), sdk.WithModeInner()); err != nil {
		observability.Warn(r.Context(), "kiosk: orders patch failed",
			append(observability.WithComponent("kiosk", observability.LayerFeature), "error", err)...)
	}
	if err := dshelpers.MergeSignals(sse, clearedSignals("Order confirmed.")); err != nil {
		observability.Warn(r.Context(), "kiosk: confirm success signals failed",
			append(observability.WithComponent("kiosk", observability.LayerFeature), "error", err)...)
	}
}

// clearedSignals empties the preview after a write. The text is absent on
// purpose: the input owns it, and clearing it from the server would delete what
// the user typed next.
func clearedSignals(notice string) map[string]any {
	return map[string]any{
		"kioskDrink":   "",
		"kioskSize":    "",
		"kioskMilk":    "",
		"kioskExtras":  "",
		"kioskPrice":   "",
		"kioskNeed":    "",
		"kioskPreview": "",
		"kioskNotice":  notice,
	}
}
