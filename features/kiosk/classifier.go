// The decision engine: a jev-like classifier settles the order.
//
// WHAT THIS IS, AND IS NOT. It is not a "fallback". The classifier is the
// decision engine, and the deterministic parser is its fast path: when the
// closed menu already matches the utterance unambiguously, asking a model would
// only add latency to a question that is already answered. When the menu does
// NOT settle it — a mixed-language phrase, "something cold with oat milk", a
// slang word — the classifier is what decides, choosing among the menu's own
// IDs.
//
// The division of labour is the point:
//
//	parser   — reads the words it knows, in microseconds, with no network;
//	classifier — reads an utterance the menu cannot, and chooses an ID.
//
// Neither invents. The classifier's criteria are the menu's IDs and an answer
// outside them is discarded, so a misheard word costs a re-ask, never a wrong
// charge. The engine is ON by default (KIOSK_CLASSIFIER_ENABLED) and reads the
// endpoint/model/key every other jev-like call uses (JEV_*).
package kiosk

import (
	"context"
	"slices"
	"strings"

	jev "github.com/mattn/go-jev"

	"github.com/calionauta/gogogo/internal/observability"
)

// The question names. One per field the menu can leave unsettled.
const (
	questionDrink = "drink"
	questionSize  = "size"
	questionMilk  = "milk"
)

// jevClient is the decision engine's client. *jev.Client satisfies it; tests
// stub it — the same 1-method seam the review judge and the chat router use.
type jevClient interface {
	Evaluate(ctx context.Context, state, questions any) (*jev.Response, error)
}

var _ jevClient = (*jev.Client)(nil)

// Classifier decides the fields the closed menu could not settle.
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

// Settle decides every field the menu left open, and reports the result.
//
// It is skipped entirely when Parse already settled the order: the parser's
// answer is deterministic and free, and re-deciding it would spend a round trip
// to reach the same place. Ambiguity is also left alone — two drinks matched,
// and asking a model which one the CUSTOMER meant is exactly the guess the
// parser refused to make; the counter staff re-ask.
func (c *Classifier) Settle(ctx context.Context, text string, parsed ParseResult) ParseResult {
	if c == nil || parsed.Complete() || parsed.Ambiguous {
		return parsed
	}
	var decided []string
	if parsed.Order.Drink == "" {
		if id, ok := c.choose(ctx, text, questionDrink, drinkIDs()); ok {
			parsed.Order.Drink = id
			decided = append(decided, questionDrink)
		}
	}
	if parsed.Order.Size == "" {
		if id, ok := c.choose(ctx, text, questionSize, sizeIDs()); ok {
			parsed.Order.Size = id
			decided = append(decided, questionSize)
		}
	}
	if parsed.Order.Milk == "" {
		if id, ok := c.choose(ctx, text, questionMilk, milkIDs()); ok {
			parsed.Order.Milk = id
			decided = append(decided, questionMilk)
		}
	}
	if len(decided) == 0 {
		return parsed
	}
	// Recompute what is still missing from the DECIDED order: the pre-engine
	// list would keep claiming fields the engine just settled.
	parsed.Missing = missingFields(parsed.Order)
	observability.Info(ctx, "kiosk: engine decided fields",
		append(observability.WithComponent("kiosk", observability.LayerFeature),
			"decided", strings.Join(decided, ","), "complete", parsed.Complete())...)
	return parsed
}

// choose asks one question and returns the chosen ID when it is confident and on
// the menu.
func (c *Classifier) choose(ctx context.Context, text, name string, ids []string) (string, bool) {
	if len(ids) == 0 {
		return "", false
	}
	criteria := make(map[string]any, len(ids))
	for _, id := range ids {
		criteria[id] = nil
	}
	resp, err := c.client.Evaluate(ctx, text, map[string]jev.Question{
		name: {
			Type:         "choice",
			Instructions: instructionsFor(name),
			Criteria:     criteria,
		},
	})
	if err != nil {
		// A transport failure must not refuse the order: the parser's "missing"
		// stands and the customer re-asks. Logged because a decision engine that
		// is unreachable is an operator problem, not a normal path.
		observability.Warn(ctx, "kiosk: engine call failed",
			append(observability.WithComponent("kiosk", observability.LayerFeature),
				"question", name, "error", err)...)
		return "", false
	}
	answer, ok := answerFor(resp, name)
	if !ok {
		return "", false
	}
	if answer.Confidence < c.threshold {
		observability.Info(ctx, "kiosk: engine below threshold",
			append(observability.WithComponent("kiosk", observability.LayerFeature),
				"question", name, "choice", answer.Value,
				"confidence", answer.Confidence, "threshold", c.threshold)...)
		return "", false
	}
	if !slices.Contains(ids, answer.Value) {
		// Confident and off-menu: the engine named an ID that does not exist.
		// Refused loudly enough to be a review signal.
		observability.Warn(ctx, "kiosk: engine chose an id outside the menu",
			append(observability.WithComponent("kiosk", observability.LayerFeature),
				"question", name, "choice", answer.Value)...)
		return "", false
	}
	return answer.Value, true
}

// answer is one normalized decision.
type answer struct {
	Value      string
	Confidence float64
}

// answerFor reads the response's answer for one question.
//
// The shape is read defensively on purpose: this is a remote service whose
// payload we did not write, and a malformed answer must degrade to "the parser's
// answer stands", never to a panic in a request.
func answerFor(resp *jev.Response, name string) (answer, bool) {
	if resp == nil || resp.Answers == nil {
		return answer{}, false
	}
	a, ok := resp.Answers[name]
	if !ok || a == nil {
		return answer{}, false
	}
	value := strings.TrimSpace(a.Choice)
	if value == "" {
		return answer{}, false
	}
	return answer{Value: value, Confidence: a.Confidence}, true
}

// instructionsFor is one sentence per question. It names the decision, not a
// rubric: the criteria carry the whole vocabulary, and an instruction that
// repeated the menu would be a second copy of it.
func instructionsFor(name string) string {
	switch name {
	case questionDrink:
		return "Which drink from the menu did the customer ask for? Choose the closest item. " +
			"If the request names no drink at all, answer with an empty choice."
	case questionSize:
		return "Which size did the customer ask for? Choose the closest one. " +
			"If no size is stated, answer with an empty choice."
	case questionMilk:
		return "Which milk did the customer ask for? Choose the closest one. " +
			"If the customer named no milk, answer with an empty choice."
	default:
		return "Choose the option that best matches what the customer asked for."
	}
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
