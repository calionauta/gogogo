// The decision engine: it settles what the menu could not, in ONE typed call,
// and never invents.
package kiosk

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	jev "github.com/mattn/go-jev"
)

// stubEngine answers from a fixed map and records HOW it was called. The call
// count is asserted: batching every field into one Evaluate is the latency
// contract, not an implementation detail.
type stubEngine struct {
	answers map[string]*jev.Answer
	err     error
	calls   int
	state   any
	asked   map[string]jev.Question
}

func (s *stubEngine) Evaluate(_ context.Context, state, questions any) (*jev.Response, error) {
	s.calls++
	s.state = state
	if qs, ok := questions.(map[string]jev.Question); ok {
		s.asked = qs
	}
	if s.err != nil {
		return nil, s.err
	}
	resp := &jev.Response{Answers: map[string]*jev.Answer{}}
	maps.Copy(resp.Answers, s.answers)
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

// TestEngineAsksEverythingInOneCall pins the batching: one sentence is one round
// trip, not one per field. The old shape asked drink, size and milk separately.
func TestEngineAsksEverythingInOneCall(t *testing.T) {
	stub := &stubEngine{answers: map[string]*jev.Answer{}}
	engine := NewClassifier(stub, 0.7)

	engine.Settle(context.Background(), "a large latte with cinnamon", Parse("a large latte with cinnamon"))

	if stub.calls != 1 {
		t.Fatalf("engine calls = %d, want 1 (all fields batched)", stub.calls)
	}
	want := make([]string, 0, 4+len(Extras))
	want = append(want, questionDrink, questionSize, questionMilk, questionOnly)
	for _, extra := range Extras {
		want = append(want, extraQuestion(extra.ID))
	}
	for _, name := range want {
		if _, ok := stub.asked[name]; !ok {
			t.Errorf("the question set is missing %q", name)
		}
	}
}

// TestEngineStateCarriesTheOrderAndUtterance is what makes a correction
// semantic: the model must see the order already on screen.
func TestEngineStateCarriesTheOrderAndUtterance(t *testing.T) {
	stub := &stubEngine{answers: map[string]*jev.Answer{}}
	engine := NewClassifier(stub, 0.7)
	draft := Order{Drink: "latte", Size: "large", Extras: []string{"cinnamon"}}

	engine.Settle(context.Background(), "remove the cinnamon",
		MergeDraft(Parse("remove the cinnamon"), draft))

	state, ok := stub.state.(engineState)
	if !ok {
		t.Fatalf("state = %T, want engineState", stub.state)
	}
	if state.Utterance != "remove the cinnamon" {
		t.Errorf("utterance = %q", state.Utterance)
	}
	for _, want := range []string{"Latte", "Large", "Cinnamon"} {
		if !strings.Contains(state.CurrentOrder, want) {
			t.Errorf("current order %q is missing %q", state.CurrentOrder, want)
		}
	}
}

// TestRetractedExtraIsNotAdded is the reported conversation: an add that is
// retracted in the same sentence resolves to the FINAL intent.
func TestRetractedExtraIsNotAdded(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		questionDrink:             {Choice: "espresso", Confidence: 0.95},
		questionOnly:              {Noul: 0.93, Confidence: 0.9},
		extraQuestion("cinnamon"): {Choice: choiceExclude, Confidence: 0.94},
	}}, 0.7)

	text := "adicione canela, ops, ah não remova canela e deixe só um expresso"
	got := engine.Settle(context.Background(), text, Parse(text))

	if got.Order.Drink != "espresso" {
		t.Errorf("drink = %q, want espresso", got.Order.Drink)
	}
	if len(got.Order.Extras) != 0 {
		t.Errorf("extras = %v, want none: the removal was retracted into the final intent", got.Order.Extras)
	}
	if got.Order.Milk != "" {
		t.Errorf("milk = %q, want empty: only the named drink survives", got.Order.Milk)
	}
}

// TestRemovalAppliesToTheDraft is the case the parser alone can never do: the
// order already has cinnamon and the new sentence takes it away.
func TestRemovalAppliesToTheDraft(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		extraQuestion("cinnamon"): {Choice: choiceExclude, Confidence: 0.93},
	}}, 0.7)
	draft := Order{Drink: "latte", Size: "large", Extras: []string{"cinnamon"}}

	got := engine.Settle(context.Background(), "remove the cinnamon",
		MergeDraft(Parse("remove the cinnamon"), draft))

	if slices.Contains(got.Order.Extras, "cinnamon") {
		t.Errorf("extras = %v, still contains the removed cinnamon", got.Order.Extras)
	}
	if got.Order.Drink != "latte" || got.Order.Size != "large" {
		t.Errorf("a field the customer did not change was lost: %+v", got.Order)
	}
}

// TestOnlyNamedDrinkClearsExtrasAndMilk is "just an espresso": the named drink
// survives, everything else the draft carried is discarded.
func TestOnlyNamedDrinkClearsExtrasAndMilk(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		questionOnly:              {Noul: 0.92, Confidence: 0.9},
		questionDrink:             {Choice: "espresso", Confidence: 0.95},
		extraQuestion("cinnamon"): {Choice: choiceInclude, Confidence: 0.9},
	}}, 0.7)
	draft := Order{Drink: "latte", Size: "large", Milk: "oat", Extras: []string{"cinnamon"}}

	got := engine.Settle(context.Background(), "just an espresso",
		MergeDraft(Parse("just an espresso"), draft))

	if got.Order.Drink != "espresso" {
		t.Errorf("drink = %q, want espresso", got.Order.Drink)
	}
	if len(got.Order.Extras) != 0 || got.Order.Milk != "" {
		t.Errorf("only-drink did not discard extras/milk: %+v", got.Order)
	}
}

// TestNoulIsReadFromTheProbability is the wire-shape bug the fallback exposed:
// a noul answer arrives with confidence 0 (the probability is the measure), so
// gating on confidence silently discarded EVERY "only the named drink" answer.
func TestNoulIsReadFromTheProbability(t *testing.T) {
	resp := &jev.Response{Answers: map[string]*jev.Answer{
		questionOnly: {Type: "noul", Noul: 0.93, Confidence: 0},
	}}
	if !confidentYes(resp, questionOnly, 0.7) {
		t.Error("a 0.93 noul answer was refused because confidence was 0")
	}
	low := &jev.Response{Answers: map[string]*jev.Answer{
		questionOnly: {Type: "noul", Noul: 0.1, Confidence: 0},
	}}
	if confidentYes(low, questionOnly, 0.7) {
		t.Error("a 0.1 noul answer was taken")
	}
}

// TestUnnamedMilkIsNotInvented is the reported misfire: "com canela" names no
// milk, and the model picked the nearest one because the criteria had no way to
// say "none". The explicit option clears instead of guessing.
func TestUnnamedMilkIsNotInvented(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		questionMilk: {Choice: milkNoneOption, Confidence: 0.93},
	}}, 0.7)
	draft := Order{Drink: "espresso", Size: "small"}

	got := engine.Settle(context.Background(), "com canela", ApplyUtterance("com canela", draft))
	if got.Order.Milk != "" {
		t.Errorf("milk = %q, want empty: the sentence named none", got.Order.Milk)
	}
}

// TestNamedMilkIsKept: the none option must not clear a milk the customer said.
func TestNamedMilkIsKept(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		questionMilk: {Choice: "oat", Confidence: 0.95},
	}}, 0.7)

	got := engine.Settle(context.Background(), "com aveia", Parse("com aveia"))
	if got.Order.Milk != "oat" {
		t.Errorf("milk = %q, want oat", got.Order.Milk)
	}
}

// TestOnlyCueResetsModifiersWhenTheUtteranceNamesNone covers the deterministic
// half: "deixe só um expresso" must drop the syrup even when the classifier is
// unsure, while "só um expresso com canela" keeps what it just asked for.
func TestOnlyCueResetsModifiersWhenTheUtteranceNamesNone(t *testing.T) {
	draft := Order{Drink: "latte", Size: "large", Milk: "oat", Extras: []string{"syrup"}}

	reset := ApplyOnlyCue("deixe só um expresso", ApplyUtterance("deixe só um expresso", draft))
	if reset.Order.Milk != "" || len(reset.Order.Extras) != 0 {
		t.Errorf("only-cue kept modifiers: %+v", reset.Order)
	}
	if reset.Order.Drink != "espresso" || reset.Order.Size != "large" {
		t.Errorf("only-cue lost the drink/size: %+v", reset.Order)
	}

	kept := ApplyOnlyCue("só um expresso com canela", ApplyUtterance("só um expresso com canela", draft))
	if !slices.Contains(kept.Order.Extras, "cinnamon") {
		t.Errorf("a named extra was dropped: %+v", kept.Order)
	}
	if kept.Order.Milk != "" {
		t.Errorf("a bare only-cue kept the draft milk: %+v", kept.Order.Milk)
	}
}

// TestOnlyCueWinsAfterTheEngine is the reported example: the engine restates the
// current milk, and the deterministic reset must still win.
func TestOnlyCueWinsAfterTheEngine(t *testing.T) {
	engine := NewClassifier(&stubEngine{answers: map[string]*jev.Answer{
		questionDrink: {Choice: "espresso", Confidence: 0.95},
		questionMilk:  {Choice: "whole", Confidence: 0.99},
	}}, 0.7)
	draft := Order{Drink: "latte", Size: "large", Milk: "whole", Extras: []string{"cinnamon"}}
	text := "adicione canela, ops, ah não remova canela e deixe só um expresso"

	parsed := engine.Settle(context.Background(), text, ApplyUtterance(text, draft))
	parsed = ApplyOnlyCue(text, parsed)

	if parsed.Order.Drink != "espresso" {
		t.Errorf("drink = %q", parsed.Order.Drink)
	}
	if parsed.Order.Milk != "" || len(parsed.Order.Extras) != 0 {
		t.Errorf("modifiers survived the only-cue after the engine: %+v", parsed.Order)
	}
}

// TestApplyUtteranceMergesTheDraft is the single seam preview and confirm share.
func TestApplyUtteranceMergesTheDraft(t *testing.T) {
	draft := Order{Drink: "latte", Size: "large"}
	got := ApplyOnlyCue("com aveia", ApplyUtterance("com aveia", draft))
	if got.Order.Drink != "latte" || got.Order.Size != "large" || got.Order.Milk != "oat" {
		t.Fatalf("ApplyUtterance = %+v, want the draft plus oat milk", got.Order)
	}
	if got.Missing != nil {
		t.Errorf("a complete order reported missing fields: %v", got.Missing)
	}
}

// TestEngineRefusesLowConfidence: below the threshold the parse stands, so the
// counter re-asks instead of acting on a guess.
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
// exactly the guess the parser refused. It must not even spend the call.
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
	if stub.calls != 0 {
		t.Errorf("the engine was called about an ambiguous order")
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

// TestNilEngineIsTheKeylessPath: a disabled engine is absence, not a failure.
func TestNilEngineIsTheKeylessPath(t *testing.T) {
	var engine *Classifier
	parsed := Parse("large latte")
	got := engine.Settle(context.Background(), "large latte", parsed)
	if got.Order.Drink != "latte" {
		t.Errorf("a nil engine changed the parse: %+v", got.Order)
	}
}
