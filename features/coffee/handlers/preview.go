// The preview half: parse the current text, answer with signals, write nothing.
package handlers

import (
	"net/http"
	"strings"

	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/coffee"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/observability"
)

// preview parses the posted text and answers with signals only: no row is
// written and no element is patched. It is the "changes while you speak" half of
// the demo, and it is deliberately the same parse the confirm runs — one
// vocabulary, one parser, two callers.
//
// The handler never echoes the text back. $coffeeText belongs to the input
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
	parsed := coffee.Parse(r.PostFormValue("text"))
	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, previewSignals(parsed)); err != nil {
		observability.Warn(r.Context(), "coffee: preview signals failed",
			append(observability.WithComponent("coffee", "handler"), "error", err)...)
	}
}

// previewSignals is the whole preview rule, as a pure function: a settled order
// shows its labels and price, an unsettled one says what is missing, an
// ambiguous one asks instead of picking. Every signal is written on every answer
// so a stale field cannot survive a correction.
func previewSignals(parsed coffee.ParseResult) map[string]any {
	signals := map[string]any{
		"coffeeDrink":   "",
		"coffeeSize":    "",
		"coffeeMilk":    "",
		"coffeeExtras":  "",
		"coffeePrice":   "",
		"coffeeNeed":    "",
		"coffeePreview": "",
	}
	if parsed.Ambiguous {
		signals["coffeeNeed"] = "I could not tell which drink — say one, or type it."
		return signals
	}
	if parsed.Order.Drink != "" {
		signals["coffeeDrink"] = coffee.DrinkLabel(parsed.Order.Drink)
		signals["coffeeSize"] = coffee.SizeLabel(parsed.Order.Size)
		signals["coffeeMilk"] = coffee.MilkLabel(parsed.Order.Milk)
		signals["coffeeExtras"] = coffee.ExtraLabels(parsed.Order.Extras)
		signals["coffeePrice"] = formatPrice(coffee.PriceCents(parsed.Order))
		signals["coffeePreview"] = coffee.Serialize(parsed.Order)
	}
	if len(parsed.Missing) > 0 {
		signals["coffeeNeed"] = "Missing: " + strings.Join(missingLabels(parsed.Missing), ", ")
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
