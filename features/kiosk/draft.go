// The order on screen, carried into the next utterance.
//
// The parser reads one sentence. A conversation has a draft: what the customer
// has already chosen. This file is the deterministic half of that — read the
// draft token back, merge it under a new parse, and recognize the words that
// make an utterance a correction rather than a fresh order. What cannot be
// decided here (a removal, a retraction) is the decision engine's job in
// classifier.go; this file only makes sure it has the draft to decide against.
package kiosk

import (
	"slices"
	"strings"
)

// Deserialize is Serialize's inverse: the preview token a page posts back,
// read into the order the next utterance is applied to. Empty or unknown
// fields stay empty; Validate is what refuses an off-menu ID at the write.
func Deserialize(token string) Order {
	token = strings.TrimSpace(token)
	if token == "" {
		return Order{}
	}
	parts := strings.Split(token, "|")
	var o Order
	if len(parts) > 0 {
		o.Drink = parts[0]
	}
	if len(parts) > 1 {
		o.Size = parts[1]
	}
	if len(parts) > 2 {
		o.Milk = parts[2]
	}
	if len(parts) > 3 && parts[3] != "" {
		o.Extras = strings.Split(parts[3], "+")
	}
	return o
}

// Empty reports whether nothing has been chosen yet.
func (o Order) Empty() bool {
	return o.Drink == "" && o.Size == "" && o.Milk == "" && len(o.Extras) == 0
}

// MergeDraft carries the order already on screen into a new parse, so a
// correction ("with oat milk", "remove the syrup") is read against what the
// customer has, not against nothing.
//
// Empty parser fields take the draft's value and extras union because the
// parser is additive. The decision engine, when it runs, then works over this
// base and is what can REMOVE an extra: the parser alone cannot.
func MergeDraft(parsed ParseResult, draft Order) ParseResult {
	if parsed.Ambiguous {
		return parsed
	}
	if parsed.Order.Drink == "" {
		parsed.Order.Drink = draft.Drink
	}
	if parsed.Order.Size == "" {
		parsed.Order.Size = draft.Size
	}
	if parsed.Order.Milk == "" {
		parsed.Order.Milk = draft.Milk
	}
	for _, id := range draft.Extras {
		parsed.Order.Extras = addExtra(parsed.Order.Extras, id)
	}
	parsed.Order.Extras = sortExtras(parsed.Order.Extras)
	parsed.Missing = missingFields(parsed.Order)
	return parsed
}

// correctionWords are the negations and refrains that make an utterance a
// CHANGE to what is already on screen. They cannot settle an order alone; they
// only tell the preview that the decision engine must look, even when the
// phrase is short ("sem canela").
var correctionWords = map[string]bool{
	"sem": true, "nao": true, "no": true, "without": true,
	"remove": true, "remova": true, "remover": true, "tira": true, "tire": true,
	"only": true, "just": true, "so": true,
	"deixe": true, "deixa": true, "actually": true, "instead": true,
}

// onlyWords are the "just that, nothing else" cues. They are the one correction
// applied deterministically as well as by the engine, because the modifier
// reset is unambiguous when the utterance does not also retract or re-add one.
var onlyWords = map[string]bool{
	"so": true, "apenas": true, "only": true, "just": true, "deixe": true, "deixa": true,
}

// removalWords are the explicit "take it out" cues. Unlike a bare only-word they
// name no replacement, so a modifier they touch is gone whatever else the
// sentence says.
var removalWords = map[string]bool{
	"sem": true, "nao": true, "no": true, "without": true,
	"remove": true, "remova": true, "remover": true, "tira": true, "tire": true,
}

// LooksLikeCorrection reports whether the utterance carries a negation or a
// refinement. Folded through the parser's own normalize, so "não" and "nao" are
// one word.
func LooksLikeCorrection(text string) bool {
	for word := range strings.FieldsSeq(normalize(text)) {
		if correctionWords[word] {
			return true
		}
	}
	return false
}

// LooksLikeOnly reports the "only the drink I named" cue.
func LooksLikeOnly(text string) bool {
	for word := range strings.FieldsSeq(normalize(text)) {
		if onlyWords[word] {
			return true
		}
	}
	return false
}

func hasRemovalCue(text string) bool {
	for word := range strings.FieldsSeq(normalize(text)) {
		if removalWords[word] {
			return true
		}
	}
	return false
}

// ApplyUtterance is the first half every caller shares: parse the utterance and
// merge the draft under it. Preview and confirm both start here, so they cannot
// disagree about what the words named.
func ApplyUtterance(text string, draft Order) ParseResult {
	return MergeDraft(Parse(text), draft)
}

// ApplyOnlyCue is the second half, run AFTER the decision engine: "just an
// espresso" discards the modifiers the customer dismissed.
//
// It is deterministic on purpose. The engine answers the same question, but a
// classifier's probability varies — the demo fallback scored 0.09 on "deixe só
// um expresso" while the words are unambiguous — and it also restates the
// current order, which would put back the milk this reset is removing. Running
// last makes the explicit cue win.
//
// A removal cue anywhere ("adicione canela, ops, remova canela, deixe só um
// expresso") clears every modifier: the sentence itself withdraws them. Without
// one, a modifier the utterance NAMES survives ("só um expresso com canela").
func ApplyOnlyCue(text string, parsed ParseResult) ParseResult {
	if parsed.Ambiguous || !LooksLikeOnly(text) {
		return parsed
	}
	raw := Parse(text)
	if hasRemovalCue(text) {
		parsed.Order.Milk = ""
		parsed.Order.Extras = nil
	} else {
		if raw.Order.Milk == "" {
			parsed.Order.Milk = ""
		}
		parsed.Order.Extras = slices.Clone(raw.Order.Extras)
	}
	parsed.Missing = missingFields(parsed.Order)
	return parsed
}

// addExtra, removeExtra and sortExtras keep the extras list a set in menu order:
// the preview, the price and the stored column all read the same list.
func addExtra(ids []string, id string) []string {
	if slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}

func removeExtra(ids []string, id string) []string {
	return slices.DeleteFunc(ids, func(x string) bool { return x == id })
}

func sortExtras(ids []string) []string {
	if len(ids) < 2 {
		return ids
	}
	rank := func(id string) int {
		for i, e := range Extras {
			if e.ID == id {
				return i
			}
		}
		return len(Extras)
	}
	slices.SortStableFunc(ids, func(a, b string) int { return rank(a) - rank(b) })
	return ids
}
