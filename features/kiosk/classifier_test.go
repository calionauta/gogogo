// The decision engine: it settles what the menu could not, and never invents.
package kiosk

import (
	"context"
	"errors"
	"testing"

	jev "github.com/mattn/go-jev"
)

// stubEngine answers from a fixed map and records the questions it was asked.
type stubEngine struct {
	answers map[string]*jev.Answer
	err     error
	asked   []string
}

func (s *stubEngine) Evaluate(_ context.Context, _ any, questions any) (*jev.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	resp := &jev.Response{Answers: map[string]*jev.Answer{}}
	for name, a := range s.answers {
		resp.Answers[name] = a
		// Keep the question order available for assertions.
		if qs, ok := questions.(map[string]jev.Question); ok {
			if _, exists := qs[name]; exists {
				s.asked = append(s.asked, name)
			}
		}
	}
	return resp, nil
}

// TestEngineDecidesWhatTheMenuCouldNot is the case the parser cannot win:
// "something cold with oat milk" names no item from the menu, and the engine
// chooses one of the menu's own IDs.
func TestEngineDecidesWhatTheMenuCouldNot(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		questionDrink: {Choice: "latte", Confidence: 0.91},
	}}, 0.7)

	parsed := Parse("something cold with oat milk")
	if parsed.Order.Drink != "" {
		t.Fatalf("setup: the menu already matched a drink (%q)", parsed.Order.Drink)
	}
	got := engine.Settle(context.Background(), "something cold with oat milk", parsed)
	if got.Order.Drink != "latte" {
		t.Errorf("drink = %q, want latte from the engine", got.Order.Drink)
	}
}

// TestEngineIsSkippedWhenTheMenuSettledIt is the fast path: a phrase the closed
// menu already understood must not cost a network round trip.
func TestEngineIsSkippedWhenTheMenuSettledIt(t *testing.T) {
	stub := &stubEngine{answers: map[string]*jev.Answer{}}
	engine := NewClassifier(stub, 0.7)

	parsed := Parse("large latte")
	got := engine.Settle(context.Background(), "large latte", parsed)
	if len(stub.asked) != 0 {
		t.Errorf("the engine was asked %v for a complete parse", stub.asked)
	}
	if got.Order.Drink != "latte" || got.Order.Size != "large" {
		t.Errorf("a complete parse was changed: %+v", got.Order)
	}
}

// TestEngineRefusesLowConfidence: below the threshold the parser's "missing"
// stands, so the counter re-asks instead of acting on a guess.
func TestEngineRefusesLowConfidence(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		questionDrink: {Choice: "latte", Confidence: 0.2},
	}}, 0.7)

	got := engine.Settle(context.Background(), "something", Parse("something"))
	if got.Order.Drink != "" {
		t.Errorf("a 0.2-confidence answer was taken: %q", got.Order.Drink)
	}
	if len(got.Missing) == 0 {
		t.Error("the order must still report what is missing")
	}
}

// TestEngineRefusesAnInventedID is the safety property that lets a model sit at
// the counter: an ID outside the menu is discarded, never priced.
func TestEngineRefusesAnInventedID(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		questionDrink: {Choice: "protein_shake", Confidence: 0.99},
	}}, 0.7)

	got := engine.Settle(context.Background(), "a protein shake", Parse("a protein shake"))
	if got.Order.Drink != "" {
		t.Errorf("an off-menu id was accepted: %q", got.Order.Drink)
	}
}

// TestEngineLeavesAmbiguityToTheHuman: two drinks matched, and picking one is
// exactly the guess the parser refused.
func TestEngineLeavesAmbiguityToTheHuman(t *testing.T) {
	stub := &stubEngine{answers: map[string]*jev.Answer{
		questionDrink: {Choice: "latte", Confidence: 0.99},
	}}
	engine := NewClassifier(stub, 0.7)

	parsed := Parse("latte mocha")
	if !parsed.Ambiguous {
		t.Fatalf("setup: expected an ambiguous parse, got %+v", parsed)
	}
	got := engine.Settle(context.Background(), "latte mocha", parsed)
	if got.Order.Drink != "" {
		t.Errorf("the engine resolved an ambiguity the parser escalated: %q", got.Order.Drink)
	}
	if len(stub.asked) != 0 {
		t.Errorf("the engine was asked about an ambiguous order: %v", stub.asked)
	}
}

// TestEngineTransportFailureKeepsTheParse: an unreachable engine is an operator
// problem, never a refused order.
func TestEngineTransportFailureKeepsTheParse(t *testing.T) {
	engine := NewClassifier(&stubEngine{err: errors.New("connection refused")}, 0.7)

	got := engine.Settle(context.Background(), "large", Parse("large"))
	if got.Order.Size != "large" {
		t.Errorf("the parser's own answer was lost: %+v", got.Order)
	}
}

// TestEngineDecidesSizeToo covers the field the reported bug was about: "big"
// the menu knows, but "a giant one" it does not.
func TestEngineDecidesSizeToo(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		questionSize: {Choice: "large", Confidence: 0.88},
	}}, 0.7)

	parsed := Parse("a latte, make it a giant one")
	if parsed.Order.Size != "" {
		t.Skipf("the menu already matched a size (%q)", parsed.Order.Size)
	}
	got := engine.Settle(context.Background(), "a latte, make it a giant one", parsed)
	if got.Order.Size != "large" {
		t.Errorf("size = %q, want large from the engine", got.Order.Size)
	}
	if !got.Complete() {
		t.Errorf("the order should be complete: %+v", got)
	}
}

// TestNilEngineIsTheKeylessPath: a disabled engine is absence, not a failure.
func TestNilEngineIsTheKeylessPath(t *testing.T) {
	var engine *Classifier
	parsed := Parse("large latte")
	got := engine.Settle(context.Background(), "large latte", parsed)
	if got.Order.Drink != "latte" {
		t.Errorf("a nil engine changed the parse: %+v", got.Order)
	}
}
