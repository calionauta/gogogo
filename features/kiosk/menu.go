// The kiosk's catalog (the demo's coffee menu) and its parser.
//
// The capability is the pipeline around this file: free text — typed or spoken
// — parsed against a CLOSED vocabulary, priced, previewed, then written as one
// owner-scoped row. This file is the data half: which items, sizes, milks and
// extras a counter sells, and what people call them. A different counter
// replaces this data; the routes, preview, confirm, page and panel stay.
//
// Two decisions live here, and both are about what NOT to add:
//
//  1. The menu is Go data, not model output and not a second list in JS. The
//     page renders the same aliases into the recognizer's contextual-bias
//     phrases, so there is one vocabulary and it cannot drift.
//  2. Parsing is a deterministic function over that closed set. A closed menu
//     does not need an LLM: it needs aliases, a unit test, and zero latency. The
//     Jev seam stays available for utterances this cannot settle, but it is
//     opt-in and measured first — see docs/research/recipe-outcomes-2026-11.md.
package kiosk

import (
	"strings"
)

// Drink is one sellable item. Aliases are matched as whole phrases, so "cha"
// does not match inside "chai".
type Drink struct {
	ID      string
	Label   string
	Aliases []string
	// BaseCents is the medium price; SizeCents adjusts it.
	BaseCents int
}

// Size is one cup size. DeltaCents moves the drink's base price.
type Size struct {
	ID         string
	Label      string
	Aliases    []string
	DeltaCents int
}

// Milk is one milk choice. The empty ID means "no milk word was said", which the
// UI shows as an em dash rather than guessing.
type Milk struct {
	ID      string
	Label   string
	Aliases []string
}

// Extra is one addition, priced once each.
type Extra struct {
	ID      string
	Label   string
	Aliases []string
	Cents   int
}

// Drinks, Sizes, Milks and Extras are the closed menu. Order is presentation
// order: the page lists them in this sequence and the parser tries aliases
// longest-first.
var (
	Drinks = []Drink{
		{ID: "espresso", Label: "Espresso", Aliases: []string{"espresso", "expresso", "short black"}, BaseCents: 350},
		{ID: "cappuccino", Label: "Cappuccino", Aliases: []string{"cappuccino", "capuccino"}, BaseCents: 450},
		{ID: "flat_white", Label: "Flat white", Aliases: []string{"flat white", "flatwhite"}, BaseCents: 470},
		{ID: "latte", Label: "Latte", Aliases: []string{"latte", "caffe latte", "cafe latte"}, BaseCents: 430},
		{ID: "mocha", Label: "Mocha", Aliases: []string{"mocha", "moca"}, BaseCents: 500},
		{ID: "tea", Label: "Tea", Aliases: []string{"tea", "black tea", "green tea"}, BaseCents: 300},
	}
	Sizes = []Size{
		{ID: "small", Label: "Small", Aliases: []string{"small", "short"}, DeltaCents: -50},
		{ID: "medium", Label: "Medium", Aliases: []string{"medium", "regular"}, DeltaCents: 0},
		{ID: "large", Label: "Large", Aliases: []string{"large", "big", "tall"}, DeltaCents: 60},
	}
	Milks = []Milk{
		{ID: "oat", Label: "Oat", Aliases: []string{"oat", "oat milk"}},
		{ID: "soy", Label: "Soy", Aliases: []string{"soy", "soy milk"}},
		{ID: "lactose_free", Label: "Lactose free", Aliases: []string{"lactose free", "lactose-free", "no lactose"}},
		{ID: "whole", Label: "Whole", Aliases: []string{"whole", "whole milk", "regular milk"}},
	}
	Extras = []Extra{
		{ID: "extra_shot", Label: "Extra shot", Aliases: []string{"extra shot", "shot extra", "double shot"}, Cents: 80},
		{ID: "syrup", Label: "Syrup", Aliases: []string{"syrup", "vanilla syrup", "caramel syrup"}, Cents: 60},
		{ID: "cinnamon", Label: "Cinnamon", Aliases: []string{"cinnamon"}, Cents: 0},
	}
)

// Order is what the parser settled and what the handler persists. Only IDs: the
// labels and the price are derived, so a stored row cannot disagree with the
// menu it was sold from.
type Order struct {
	Drink  string
	Size   string
	Milk   string
	Extras []string
}

// ParseResult is the outcome of reading one utterance. Missing names the fields
// a customer still has to settle; Ambiguous means two drinks matched and
// guessing would be worse than asking.
type ParseResult struct {
	Order     Order
	Missing   []string
	Ambiguous bool
}

// Complete reports whether the order can be confirmed as parsed.
func (p ParseResult) Complete() bool {
	return !p.Ambiguous && len(p.Missing) == 0
}

// Parse reads one utterance against the closed menu. It never invents a value:
// anything outside the vocabulary is ignored, and an unsettled field is reported
// in Missing instead of being defaulted.
func Parse(text string) ParseResult {
	norm := normalize(text)
	var res ParseResult

	drinks := matchAll(norm, drinkVocabulary())
	switch {
	case len(drinks) == 1:
		res.Order.Drink = drinks[0]
	case len(drinks) > 1:
		res.Ambiguous = true
	}

	if sizes := matchAll(norm, sizeVocabulary()); len(sizes) == 1 {
		res.Order.Size = sizes[0]
	}
	if milks := matchAll(norm, milkVocabulary()); len(milks) == 1 {
		res.Order.Milk = milks[0]
	}
	res.Order.Extras = matchAll(norm, extraVocabulary())

	switch {
	case res.Order.Drink == "" && !res.Ambiguous:
		res.Missing = append(res.Missing, "drink")
	case res.Order.Size == "":
		res.Missing = append(res.Missing, "size")
	}
	return res
}

// Serialize is the preview's identity: what the customer was shown. The confirm
// handler compares it with the order it parses to decide `corrected`, which is
// the counter signal the outcome reads.
func Serialize(o Order) string {
	return strings.Join([]string{o.Drink, o.Size, o.Milk, strings.Join(o.Extras, "+")}, "|")
}

// Validate re-checks a parsed order against the menu. The parser only produces
// known IDs, but a confirm can arrive from a form the browser built, so the
// service costs nothing and closes the "client supplied an ID" path.
func Validate(o Order) []string {
	var problems []string
	if _, ok := LookupDrink(o.Drink); !ok {
		problems = append(problems, "drink is not on the menu")
	}
	if _, ok := LookupSize(o.Size); !ok {
		problems = append(problems, "size is not on the menu")
	}
	if o.Milk != "" {
		if _, ok := LookupMilk(o.Milk); !ok {
			problems = append(problems, "milk is not on the menu")
		}
	}
	for _, e := range o.Extras {
		if _, ok := LookupExtra(e); !ok {
			problems = append(problems, "extra "+e+" is not on the menu")
		}
	}
	return problems
}

// PriceCents is the order's price: the drink's base, the size adjustment, and
// each extra once.
func PriceCents(o Order) int {
	drink, ok := LookupDrink(o.Drink)
	if !ok {
		return 0
	}
	total := drink.BaseCents
	if size, ok := LookupSize(o.Size); ok {
		total += size.DeltaCents
	}
	for _, id := range o.Extras {
		if extra, ok := LookupExtra(id); ok {
			total += extra.Cents
		}
	}
	if total < 0 {
		return 0
	}
	return total
}

// DrinkLabel, SizeLabel, MilkLabel and ExtraLabels render stored IDs for the page
// and the chat panel. An unknown ID renders as itself: a row the menu no longer
// lists is still a fact the user should see.
func DrinkLabel(id string) string {
	return labelOf(id, Drinks, func(d Drink) (string, string) { return d.ID, d.Label })
}

func SizeLabel(id string) string {
	return labelOf(id, Sizes, func(s Size) (string, string) { return s.ID, s.Label })
}

func MilkLabel(id string) string {
	return labelOf(id, Milks, func(m Milk) (string, string) { return m.ID, m.Label })
}

func ExtraLabels(ids []string) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, labelOf(id, Extras, func(e Extra) (string, string) { return e.ID, e.Label }))
	}
	return strings.Join(out, ", ")
}

// labelOf is the one ID-to-label lookup: empty stays empty (an unset milk is
// shown as an em dash by the view, not as the word "whole"), and an unknown ID
// renders as itself.
func labelOf[T any](id string, items []T, key func(T) (string, string)) string {
	if id == "" {
		return ""
	}
	for _, item := range items {
		if k, label := key(item); k == id {
			return label
		}
	}
	return id
}

// LookupDrink, LookupSize, LookupMilk and LookupExtra are the closed-set lookups
// the validator and the price share.
func LookupDrink(id string) (Drink, bool) {
	for _, d := range Drinks {
		if d.ID == id {
			return d, true
		}
	}
	return Drink{}, false
}

func LookupSize(id string) (Size, bool) {
	for _, s := range Sizes {
		if s.ID == id {
			return s, true
		}
	}
	return Size{}, false
}

func LookupMilk(id string) (Milk, bool) {
	for _, m := range Milks {
		if m.ID == id {
			return m, true
		}
	}
	return Milk{}, false
}

func LookupExtra(id string) (Extra, bool) {
	for _, e := range Extras {
		if e.ID == id {
			return e, true
		}
	}
	return Extra{}, false
}

// Phrases is the recognizer's contextual bias: the drink names, exactly as the
// menu spells them. Speech recognition accepts a vocabulary guess far better
// than a post-hoc correction, and this is the only place the list is written.
func Phrases() []string {
	out := make([]string, 0, len(Drinks))
	for _, d := range Drinks {
		out = append(out, d.Label)
	}
	return out
}

// vocabulary is one alias set: the alias and the ID it resolves to.
type vocabulary struct {
	alias string
	id    string
}

// The four vocabularies are built from the exported slices, never from a second
// list: the alias a customer can say and the ID the row stores are one fact.

func drinkVocabulary() []vocabulary {
	out := make([]vocabulary, 0, len(Drinks))
	for _, d := range Drinks {
		out = append(out, aliasesOf(d.ID, d.Aliases)...)
	}
	return out
}

func sizeVocabulary() []vocabulary {
	out := make([]vocabulary, 0, len(Sizes))
	for _, s := range Sizes {
		out = append(out, aliasesOf(s.ID, s.Aliases)...)
	}
	return out
}

func milkVocabulary() []vocabulary {
	out := make([]vocabulary, 0, len(Milks))
	for _, m := range Milks {
		out = append(out, aliasesOf(m.ID, m.Aliases)...)
	}
	return out
}

func extraVocabulary() []vocabulary {
	out := make([]vocabulary, 0, len(Extras))
	for _, e := range Extras {
		out = append(out, aliasesOf(e.ID, e.Aliases)...)
	}
	return out
}

func aliasesOf(id string, aliases []string) []vocabulary {
	out := make([]vocabulary, 0, len(aliases))
	for _, a := range aliases {
		if norm := normalize(a); norm != "" {
			out = append(out, vocabulary{alias: norm, id: id})
		}
	}
	return out
}

// matchAll returns every distinct ID whose alias appears in the normalized text,
// in vocabulary order. Two different IDs matching is ambiguity, not a tie to
// break: the caller asks instead of picking.
func matchAll(norm string, vocab []vocabulary) []string {
	if norm == "" {
		return nil
	}
	padded := " " + norm + " "
	seen := map[string]bool{}
	var out []string
	for _, v := range vocab {
		if v.alias == "" || !strings.Contains(padded, " "+v.alias+" ") {
			continue
		}
		if seen[v.id] {
			continue
		}
		seen[v.id] = true
		out = append(out, v.id)
	}
	return out
}

// normalize lowercases, folds the accents a recognizer may emit on a loanword,
// drops punctuation and collapses whitespace. A hand-written replacer, not a
// normalization dependency: the vocabulary is closed and small, and the parser
// is easier to read when the exact substitutions are visible.
var accentFolder = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
	"é", "e", "è", "e", "ê", "e",
	"í", "i", "ì", "i", "î", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n",
)

func normalize(s string) string {
	lower := accentFolder.Replace(strings.ToLower(s))
	var b strings.Builder
	space := false
	for _, r := range lower {
		if isWordRune(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		// Anything else (spaces, punctuation, quotes, dashes, emoji) separates
		// words rather than joining them, which is what a spoken sentence does.
		if !space && b.Len() > 0 {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

// isWordRune keeps what the closed vocabulary and its aliases are spelled with.
func isWordRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_'
}
