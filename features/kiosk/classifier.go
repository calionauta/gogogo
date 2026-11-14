// The decision engine: a jev-like classifier settles the order.
//
// WHAT THIS IS, AND IS NOT. It is not a "fallback". The classifier is the
// decision engine, and the deterministic parser is its fast path: when the
// closed menu already matches the utterance, asking a model would only add
// latency to a question that is already answered. When the menu does NOT settle
// it — a mixed-language phrase, "something cold with oat milk", a correction
// like "remove the cinnamon" — the classifier is what decides, choosing among
// the menu's own IDs.
//
// The division of labour is the point:
//
//	parser   — reads the words it knows, in microseconds, with no network;
//	classifier — reads what the words MEAN: an add, a removal, a retraction.
//
// Neither invents. The classifier's criteria are the menu's own IDs and an
// answer outside them is discarded, so a misheard word costs a re-ask, never a
// wrong charge. The engine is ON by default (KIOSK_CLASSIFIER_ENABLED) and reads
// the endpoint/model/key every other jev-like call uses (JEV_*).
//
// ONE CALL, TYPED DECISIONS. Every field is asked in a single Evaluate: a
// `choice` for the drink, the size, the milk and each extra, plus one `noul`
// (yes/no probability) for "only the drink they named". Asking per field in
// separate calls was up to four round trips for one sentence; the decision API
// answers a whole question set at once. The state carries the order already on
// screen, so "remove the cinnamon" has a cinnamon to remove and a retracted
// "add cinnamon" resolves to the final intent, not to the word that was undone.
package kiosk

import (
	"context"
	"slices"
	"strings"

	jev "github.com/mattn/go-jev"

	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/prompts"
)

// The question names. One per decision the engine makes.
const (
	questionDrink = "drink"
	questionSize  = "size"
	questionMilk  = "milk"
	questionOnly  = "only_named_drink"
	extraPrefix   = "extra_"
	choiceInclude = "include"
	choiceExclude = "exclude"
	promptID      = "kiosk/decide"

	typeChoice   = "choice"
	typeNoul     = "noul"
	yesThreshold = 0.5
)

// jevClient is the decision engine's client. *jev.Client satisfies it; tests
// stub it — the same 1-method seam the review judge and the chat router use.
type jevClient interface {
	Evaluate(ctx context.Context, state, questions any) (*jev.Response, error)
}

var _ jevClient = (*jev.Client)(nil)

// engineState is what the model reasons over: the order as it stands and the
// one new utterance to apply to it. Sending the draft is what makes a
// correction semantic rather than a fresh parse.
type engineState struct {
	CurrentOrder string `json:"currentOrder"`
	Utterance    string `json:"utterance"`
}

// Classifier decides the customer's final order.
type Classifier struct {
	client    jevClient
	threshold float64
}

// NewClassifier wraps a client. A nil client returns nil, which means "no engine
// configured" — the parser-only path, not a broken engine.
func NewClassifier(client jevClient, threshold float64) *Classifier {
	if client == nil {
		return nil
	}
	return &Classifier{client: client, threshold: threshold}
}

// Settle decides the final order from the working order and the utterance.
//
// The caller has already merged the order on screen into parsed (MergeDraft), so
// `parsed.Order` is the draft this utterance applies to. Ambiguity is left alone:
// two drinks matched, and asking a model which one the CUSTOMER meant is exactly
// the guess the parser refused to make; the counter staff re-ask.
func (c *Classifier) Settle(ctx context.Context, text string, parsed ParseResult) ParseResult {
	if c == nil || parsed.Ambiguous {
		return parsed
	}
	resp, err := c.client.Evaluate(ctx,
		engineState{CurrentOrder: describeOrder(parsed.Order), Utterance: text},
		decisionQuestions())
	if err != nil {
		// A transport failure must not refuse the order: the merged parse stands
		// and the customer re-asks. Logged because an unreachable decision engine
		// is an operator problem, not a normal path.
		observability.Warn(ctx, "kiosk: decision engine call failed",
			append(observability.WithComponent("kiosk", observability.LayerFeature),
				"error", err)...)
		return parsed
	}

	order, decided := c.applyDecisions(ctx, resp, parsed.Order)
	parsed.Order = order
	parsed.Missing = missingFields(order)
	if len(decided) > 0 {
		observability.Info(ctx, "kiosk: engine decided fields",
			append(observability.WithComponent("kiosk", observability.LayerFeature),
				"decided", strings.Join(decided, ","), "complete", parsed.Complete())...)
	}
	return parsed
}

// applyDecisions is the pure reducer: it applies the typed answers to the
// working order and reports which fields changed. Kept out of Settle so the
// request path stays one straight line.
func (c *Classifier) applyDecisions(ctx context.Context, resp *jev.Response, order Order) (Order, []string) {
	var decided []string
	if id, ok := c.pick(ctx, resp, questionDrink, drinkIDs()); ok && id != order.Drink {
		order.Drink = id
		decided = append(decided, questionDrink)
	}
	if id, ok := c.pick(ctx, resp, questionSize, sizeIDs()); ok && id != order.Size {
		order.Size = id
		decided = append(decided, questionSize)
	}
	order, milkChanged := c.applyMilk(ctx, resp, order)
	if milkChanged {
		decided = append(decided, questionMilk)
	}
	order, extraChanges := c.applyExtras(ctx, resp, order)
	decided = append(decided, extraChanges...)
	// "Just an espresso" discards everything that is not the named drink. It runs
	// last so it wins over a field the model also restated from the current order.
	if confidentYes(resp, questionOnly, c.threshold) && (len(order.Extras) > 0 || order.Milk != "") {
		order.Extras = nil
		order.Milk = ""
		decided = append(decided, questionOnly)
	}
	order.Extras = sortExtras(order.Extras)
	return order, decided
}

// applyMilk is the milk's part of the reducer: a named milk wins, the explicit
// "none" clears, anything unsaid leaves the draft as it was.
func (c *Classifier) applyMilk(ctx context.Context, resp *jev.Response, order Order) (Order, bool) {
	switch c.milkDecision(ctx, resp) {
	case milkNone:
		if order.Milk != "" {
			order.Milk = ""
			return order, true
		}
	case milkNamed:
		if id, ok := c.pick(ctx, resp, questionMilk, milkIDs()); ok && id != order.Milk {
			order.Milk = id
			return order, true
		}
	}
	return order, false
}

// applyExtras is the extras' part of the reducer: one include/exclude per extra.
func (c *Classifier) applyExtras(ctx context.Context, resp *jev.Response, order Order) (Order, []string) {
	var decided []string
	for _, extra := range Extras {
		switch c.extraDecision(ctx, resp, extra) {
		case extraInclude:
			if !slices.Contains(order.Extras, extra.ID) {
				order.Extras = addExtra(order.Extras, extra.ID)
				decided = append(decided, extra.ID+"+")
			}
		case extraExclude:
			if slices.Contains(order.Extras, extra.ID) {
				order.Extras = removeExtra(order.Extras, extra.ID)
				decided = append(decided, extra.ID+"-")
			}
		}
	}
	return order, decided
}

// decisionQuestions is the whole question set for one utterance. The criteria
// carry the menu's own vocabulary (id plus human label), so the model can map a
// word the parser does not know — "canela" to the Cinnamon option — without a
// second copy of the menu in a prompt.
func decisionQuestions() map[string]jev.Question {
	questions := map[string]jev.Question{
		questionDrink: {
			Type:         typeChoice,
			Instructions: instructionsFor("the drink"),
			Criteria:     choiceOptions(Drinks, func(d Drink) (string, string) { return d.ID, d.Label }),
		},
		questionSize: {
			Type:         typeChoice,
			Instructions: instructionsFor("the size"),
			Criteria:     choiceOptions(Sizes, func(s Size) (string, string) { return s.ID, s.Label }),
		},
		questionMilk: {
			Type:         typeChoice,
			Instructions: instructionsFor("the milk"),
			// An explicit "none" is what keeps a sentence that names no milk from
			// selecting the nearest one. The criteria are the menu plus this one
			// answer, which the reducer maps to "no milk", never to an ID.
			Criteria: append(
				choiceOptions(Milks, func(m Milk) (string, string) { return m.ID, m.Label }),
				jev.Option{Name: milkNoneOption, Desc: "the customer named no milk"},
			),
		},
		questionOnly: {
			Type: typeNoul,
			Instructions: instructionsFor(
				"whether the customer wants only the drink they just named, discarding extras and milk"),
		},
	}
	for _, extra := range Extras {
		questions[extraQuestion(extra.ID)] = jev.Question{
			Type:         typeChoice,
			Instructions: instructionsFor("whether the final order includes " + extra.Label),
			Criteria: jev.Options{
				{Name: choiceInclude, Desc: "the final order includes it"},
				{Name: choiceExclude, Desc: "the final order leaves it out"},
			},
		}
	}
	return questions
}

// choiceOptions projects a menu slice into the labelled options the API takes.
func choiceOptions[T any](items []T, project func(T) (string, string)) jev.Options {
	out := make(jev.Options, 0, len(items))
	for _, item := range items {
		id, label := project(item)
		out = append(out, jev.Option{Name: id, Desc: label})
	}
	return out
}

func extraQuestion(id string) string { return extraPrefix + id }

// instructionsFor renders the one model-facing framing (a prompt file) with the
// field being decided. The prose lives in internal/prompts/files.
func instructionsFor(field string) string {
	return prompts.MustRender(promptID, map[string]string{"field": field})
}

// pick reads a choice answer and returns the ID when it is confident and on the
// menu. An empty choice is "no opinion" and never clears a field.
func (c *Classifier) pick(ctx context.Context, resp *jev.Response, name string, ids []string) (string, bool) {
	if len(ids) == 0 {
		return "", false
	}
	a := answerFor(resp, name)
	if a == nil {
		return "", false
	}
	value := strings.TrimSpace(a.Choice)
	if value == "" {
		return "", false
	}
	if a.Confidence < c.threshold {
		observability.Info(ctx, "kiosk: engine below threshold",
			append(observability.WithComponent("kiosk", observability.LayerFeature),
				"question", name, "choice", value,
				"confidence", a.Confidence, "threshold", c.threshold)...)
		return "", false
	}
	if !slices.Contains(ids, value) {
		// Confident and off-menu: the engine named an ID that does not exist.
		// Refused loudly enough to be a review signal.
		observability.Warn(ctx, "kiosk: engine chose an id outside the menu",
			append(observability.WithComponent("kiosk", observability.LayerFeature),
				"question", name, "choice", value)...)
		return "", false
	}
	return value, true
}

// milkNoneOption is the milk question's explicit "no milk named" answer. It is
// not a menu ID (the empty milk is "not stated"), so it never reaches the order
// as a value; it only distinguishes "nothing said" from the closest milk.
const milkNoneOption = "none"

// milkDecisionKind is the milk question's answer class.
type milkDecisionKind int

const (
	milkUnsaid milkDecisionKind = iota
	milkNamed
	milkNone
)

// milkDecision classifies the milk answer: a menu ID means a milk, the explicit
// none option means clear, anything else (empty, off-menu, low confidence)
// leaves the field as the draft had it.
func (c *Classifier) milkDecision(ctx context.Context, resp *jev.Response) milkDecisionKind {
	a := answerFor(resp, questionMilk)
	if a == nil {
		return milkUnsaid
	}
	value := strings.TrimSpace(a.Choice)
	if value == "" || a.Confidence < c.threshold {
		return milkUnsaid
	}
	if value == milkNoneOption {
		return milkNone
	}
	if _, ok := LookupMilk(value); !ok {
		observability.Warn(ctx, "kiosk: engine chose a milk outside the menu",
			append(observability.WithComponent("kiosk", observability.LayerFeature),
				"question", questionMilk, "choice", value)...)
		return milkUnsaid
	}
	return milkNamed
}

// extraDecision is one extra's include/exclude decision.
type extraDecisionKind int

const (
	extraNone extraDecisionKind = iota
	extraInclude
	extraExclude
)

func (c *Classifier) extraDecision(ctx context.Context, resp *jev.Response, extra Extra) extraDecisionKind {
	a := answerFor(resp, extraQuestion(extra.ID))
	if a == nil {
		return extraNone
	}
	value := strings.TrimSpace(a.Choice)
	if value == "" || a.Confidence < c.threshold {
		return extraNone
	}
	switch value {
	case choiceInclude:
		return extraInclude
	case choiceExclude:
		return extraExclude
	default:
		observability.Warn(ctx, "kiosk: engine answered an extra with an unknown decision",
			append(observability.WithComponent("kiosk", observability.LayerFeature),
				"extra", extra.ID, "choice", value)...)
		return extraNone
	}
}

// confidentYes reads a noul answer. Noul IS the probability of "yes" — the
// server returns it with confidence 0, because a probability is already the
// measure a choice spells as confidence. Gating on confidence here silently
// discarded every noul answer (the "only the named drink" decision never
// applied); the probability is compared to the threshold instead.
func confidentYes(resp *jev.Response, name string, threshold float64) bool {
	a := answerFor(resp, name)
	if a == nil {
		return false
	}
	return a.Noul >= max(threshold, yesThreshold)
}

// answerFor reads the response's answer for one question defensively: this is a
// remote service whose payload we did not write, and a malformed answer must
// degrade to "the parse stands", never to a panic in a request.
func answerFor(resp *jev.Response, name string) *jev.Answer {
	if resp == nil || resp.Answers == nil {
		return nil
	}
	return resp.Answers[name]
}

// describeOrder renders the working order for the model, labels rather than IDs.
func describeOrder(o Order) string {
	var parts []string
	if o.Drink != "" {
		parts = append(parts, DrinkLabel(o.Drink))
	}
	if o.Size != "" {
		parts = append(parts, SizeLabel(o.Size))
	}
	if o.Milk != "" {
		parts = append(parts, MilkLabel(o.Milk))
	}
	if extras := ExtraLabels(o.Extras); extras != "" {
		parts = append(parts, extras)
	}
	if len(parts) == 0 {
		return "empty"
	}
	return strings.Join(parts, ", ")
}

// drinkIDs, sizeIDs and milkIDs are the closed sets the engine may choose from —
// read from the same menu data the parser uses, so a new item reaches both with
// one edit.
func drinkIDs() []string { return idsOf(Drinks, func(d Drink) string { return d.ID }) }
func sizeIDs() []string  { return idsOf(Sizes, func(s Size) string { return s.ID }) }
func milkIDs() []string  { return idsOf(Milks, func(m Milk) string { return m.ID }) }

// idsOf projects a menu slice to its ids.
func idsOf[T any](items []T, id func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, id(item))
	}
	return out
}

// missingFields is the menu's own completeness rule, recomputed for a decided
// order so the engine cannot leave a stale "missing" list behind.
func missingFields(o Order) []string {
	var missing []string
	if o.Drink == "" {
		missing = append(missing, "drink")
	}
	if o.Size == "" {
		missing = append(missing, "size")
	}
	return missing
}
