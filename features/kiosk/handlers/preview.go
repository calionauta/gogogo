// The preview half: parse the current text, answer with signals, write nothing.
package handlers

import (
	"net/http"
	"strings"

	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/kiosk"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/observability"
)

// preview parses the posted text and answers with signals only: no row is
// written and no element is patched. It is the "changes while you speak" half of
// the demo, and it is deliberately the same parse the confirm runs — one
// vocabulary, one parser, two callers.
//
// The handler never echoes the text back. $kioskText belongs to the input
// (data-bind), so a server write would clobber what the user is typing — the bug
// docs/realtime-recipe.md records for the todo search box.
func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r); !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	// The parser reads what the menu knows; the engine decides the rest.
	//
	// `previewed` is the order already on screen: the draft this utterance is
	// applied to. Carrying it in is what lets a correction mean something on a
	// later sentence — "remove the cinnamon" needs a cinnamon to remove. An
	// empty value is a fresh order.
	//
	// The field IS interpreted: no button tells the server to understand it. The
	// engine runs when the menu could not settle the final intent or the
	// utterance is a change to an order already in play; a plain "large latte"
	// costs no round trip. That condition is the latency budget, so it does not
	// need a flag the page has to remember to set.
	text := r.PostFormValue("text")
	draft := kiosk.Deserialize(r.PostFormValue("previewed"))
	parsed := kiosk.ApplyUtterance(text, draft)
	if h.engine != nil && engineWorthAsking(parsed, draft, text) {
		parsed = h.engine.Settle(r.Context(), text, parsed)
	}
	// The explicit "just this" cue runs after the engine: it restates the
	// current order, which would put back the modifier the customer dismissed.
	parsed = kiosk.ApplyOnlyCue(text, parsed)
	// A tapped choice is an EXPLICIT decision and wins over whatever the text
	// implied: the customer pointed at "Oat", so a stale "whole" from an earlier
	// sentence must not survive. The merge is one place (applyChoices) so the
	// preview and the confirm agree.
	parsed = kiosk.ApplyChoices(parsed, kiosk.Choices{
		Drink:  r.PostFormValue("drink"),
		Size:   r.PostFormValue("size"),
		Milks:  r.PostForm["milk"],
		Extras: r.PostForm["extras"],
	})
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, previewSignals(parsed)); err != nil {
		observability.Warn(r.Context(), "kiosk: preview signals failed",
			append(observability.WithComponent("kiosk", observability.LayerFeature), "error", err)...)
	}
}

// previewSignals is the whole preview rule, as a pure function: a settled order
// shows its labels and price, an unsettled one says what is missing, an
// ambiguous one asks instead of picking. Every signal is written on every answer
// so a stale field cannot survive a correction.
func previewSignals(parsed kiosk.ParseResult) map[string]any {
	signals := map[string]any{
		"kioskDrink":   "",
		"kioskSize":    "",
		"kioskMilk":    "",
		"kioskExtras":  "",
		"kioskPrice":   "",
		"kioskNeed":    "",
		"kioskPreview": "",
	}
	if parsed.Ambiguous {
		signals["kioskNeed"] = "I could not tell which drink — say one, or type it."
		return signals
	}
	// Partial orders show what IS known. Gating every field on the drink meant
	// "I'd like a big drink" left the whole preview blank: the size was parsed
	// and then thrown away, so the page looked frozen while the user spoke. Only
	// the price and the preview token wait for a drink — a size with no drink has
	// no price, and a partial preview must never look confirmable.
	if parsed.Order.Drink != "" {
		signals["kioskDrink"] = kiosk.DrinkLabel(parsed.Order.Drink)
		signals["kioskPrice"] = formatPrice(kiosk.PriceCents(parsed.Order))
		signals["kioskPreview"] = kiosk.Serialize(parsed.Order)
	}
	signals["kioskSize"] = kiosk.SizeLabel(parsed.Order.Size)
	signals["kioskMilk"] = kiosk.MilkLabel(parsed.Order.Milk)
	signals["kioskExtras"] = kiosk.ExtraLabels(parsed.Order.Extras)
	if len(parsed.Missing) > 0 {
		signals["kioskNeed"] = "Missing: " + strings.Join(missingLabels(parsed.Missing), ", ")
	}
	return signals
}

// missingLabels renders the parser's field names in the page's words. A closed
// map, so a new missing field shows up as a problem in review rather than as an
// internal token in user-facing copy.
func missingLabels(missing []string) []string {
	words := map[string]string{"drink": "a drink", "size": "a size"}
	out := make([]string, 0, len(missing))
	for _, field := range missing {
		if word, ok := words[field]; ok {
			out = append(out, word)
			continue
		}
		out = append(out, field)
	}
	return out
}

// engineWorthAsking is the latency budget, stated once. It answers "could a
// classifier change this order, and is the sentence finished enough to ask
// about?" — not "is the engine enabled", which config already decides.
func engineWorthAsking(parsed kiosk.ParseResult, draft kiosk.Order, text string) bool {
	if parsed.Ambiguous {
		return false
	}
	words := len(strings.Fields(text))
	if words == 0 {
		return false
	}
	// A correction, an extra in play, or an order already on screen is semantic
	// even in two words: "sem canela" and "remova canela" must reach the engine,
	// or the parser's additive match would add what the customer just removed.
	if kiosk.LooksLikeCorrection(text) || !draft.Empty() || len(parsed.Order.Extras) > 0 {
		return words >= 2
	}
	// Otherwise: fewer than three words is a phrase still being composed, and a
	// complete parse needs nothing. Asking here would spend a round trip to
	// classify "a big" and throw the answer away on the next keystroke.
	return words >= 3 && !parsed.Complete()
}
