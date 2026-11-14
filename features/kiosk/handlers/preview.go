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
	// The parser reads what the menu knows; the engine decides the rest. Settle
	// is a no-op when the parse is already complete, so the fast path costs
	// nothing.
	//
	// The field IS interpreted: no button tells the server to understand it.
	// The engine runs only when the menu could NOT settle the utterance, and
	// never for a parse that is already complete — a plain "large latte" costs
	// no round trip, while an unfamiliar phrase gets the decision engine. That
	// condition is the latency budget, so it does not need a second flag the
	// page has to remember to set.
	text := r.PostFormValue("text")
	parsed := kiosk.Parse(text)
	// The engine is the expensive leg (~1s per question), so it runs only when
	// it can change the answer and the customer has stopped composing:
	//
	//   - the menu left something unsettled (a complete parse needs nothing);
	//   - the utterance is long enough to be a sentence rather than the first
	//     letters of one ("a big" is still being typed, "a big espresso with
	//     milk" is a request);
	//   - the catalogue is not mid-tap (it sends explicit IDs, not words).
	//
	// `engine` is therefore the page telling us "the input is settled" (blur,
	// mic final, or the debounce after a pause), never a button the customer has
	// to press to be understood.
	if h.engine != nil && r.PostFormValue("engine") != "0" && engineWorthAsking(parsed, text) {
		parsed = h.engine.Settle(r.Context(), text, parsed)
	}
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
func engineWorthAsking(parsed kiosk.ParseResult, text string) bool {
	if parsed.Complete() || parsed.Ambiguous {
		return false
	}
	// Fewer than three words is a phrase still being composed. Asking here
	// would spend a second to classify "a big" and then throw the answer away
	// on the next keystroke.
	return len(strings.Fields(text)) >= 3
}
