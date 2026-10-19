// SCOPE:plugin — per-instance ids and the declared-action gate (red-proof).
//
// Two reported problems, one root each:
//
//  1. "todo componente deve ter um ID único pra nunca se repetir" — components
//     rendered with a fixed id (`#todo-panel`) or none at all, so a page
//     holding two answers (a chat thread keeps its history) produced duplicate
//     DOM ids and a selector that silently meant "the first match".
//  2. "plan_cards é do catálogo, por que só ele?" — it carried a hardcoded
//     feature endpoint. Making the action a prop fixes the layer violation but
//     leaves the model writing a URL, so the action must be DECLARED by a
//     feature and validated against the declaration.
package components

import (
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// TestRenderAllGivesEveryComponentItsOwnID is the red-proof for (1): two
// answers rendered onto one page must not share an id. Same directives, two
// prefixes — which is exactly what a chat reload does (every stored answer
// re-renders under its own prefix).
func TestRenderAllGivesEveryComponentItsOwnID(t *testing.T) {
	t.Parallel()
	// A FRESH directive list per answer, because a directive keeps the id it was
	// given: reusing one slice for both prefixes would test the fixture, not the
	// prefixing. In production each answer is parsed (and its ids minted)
	// separately, then rendered under its own message prefix.
	build := func() []Directive {
		return []Directive{
			{Component: "text_note", Props: map[string]any{"text": "one"}},
			{Component: "text_note", Props: map[string]any{"text": "two"}},
		}
	}
	seen := map[string]bool{}
	for _, tag := range []string{"answer-a", "answer-b"} {
		for _, id := range idsInRender(t, build(), tag) {
			if seen[id] {
				t.Fatalf("id %q rendered twice across two answers — duplicate DOM id", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 4 {
		t.Fatalf("got %d distinct ids, want 4 (two components x two answers)", len(seen))
	}
}

// idsInRender renders the directives under one prefix and returns the ids.
func idsInRender(t *testing.T, dirs []Directive, prefix string) []string {
	t.Helper()
	comps, err := RenderAllWithPrefix(dirs, prefix)
	if err != nil {
		t.Fatalf("RenderAllWithPrefix(%q): %v", prefix, err)
	}
	var b strings.Builder
	for _, c := range comps {
		if rerr := c.Render(t.Context(), &b); rerr != nil {
			t.Fatalf("render: %v", rerr)
		}
	}
	out := []string{}
	rest := b.String()
	for {
		i := strings.Index(rest, `id="`)
		if i < 0 {
			break
		}
		rest = rest[i+4:]
		j := strings.Index(rest, `"`)
		if j < 0 {
			break
		}
		out = append(out, rest[:j])
		rest = rest[j:]
	}
	return out
}

// TestInstanceIDIsServerOwned is the red-proof for the trust boundary: a model
// that sends an id must not be able to choose it. Ids become Datastar selectors
// and DOM ids, so a model-chosen one is a way to aim a patch at any element.
func TestInstanceIDIsServerOwned(t *testing.T) {
	t.Parallel()
	dirs, err := ParseDirectives(`{"components":[{"type":"text_note","props":{"text":"hi","_id":"chat-messages"}}]}`)
	if err != nil {
		t.Fatalf("a model-emitted _id must be ignored, not fatal: %v", err)
	}
	got := dirs[0].InstanceID()
	if got == "chat-messages" {
		t.Fatal("the model chose its own id — a selector it controls")
	}
	if got == "" {
		t.Fatal("no id was assigned at parse time")
	}
}

// TestValidInstanceID pins the character rule the id shares with the selector
// that will address it: a permissive rule would push escaping into every query.
func TestValidInstanceID(t *testing.T) {
	t.Parallel()
	longest := strings.Repeat("a", maxInstanceIDLen)
	for _, ok := range []string{"a", "genui-abc123", "A_1-x", longest} {
		if !validInstanceID(ok) {
			t.Errorf("validInstanceID(%q) = false, want true", ok)
		}
	}
	tooLong := strings.Repeat("a", maxInstanceIDLen+1)
	for _, bad := range []string{"", "1leading", "has space", "quote\"", "a>", tooLong} {
		if validInstanceID(bad) {
			t.Errorf("validInstanceID(%q) = true, want false", bad)
		}
	}
}

// TestUndeclaredActionIsRejected is the red-proof for (2): the shape check
// alone (same-origin path, POST) would accept any existing route, so a card
// could call an endpoint no feature offered to the model. Only a declared
// action passes.
func TestUndeclaredActionIsRejected(t *testing.T) {
	t.Parallel()
	undeclared := `{"components":[{"type":"plan_cards","props":{"plans":[
		{"title":"Ship it","action":{"label":"Apply","method":"POST","url":"/api/not-declared","fields":{}}}]}}]}`
	if _, err := ParseDirectives(undeclared); err == nil {
		t.Fatal("an action no feature declared was accepted")
	}

	declared := `{"components":[{"type":"plan_cards","props":{"plans":[
		{"title":"Ship it","action":{"label":"Apply","method":"POST","url":"/api/todos","fields":{"title":"Ship it"}}}]}}]}`
	dirs, err := ParseDirectives(declared)
	if err != nil {
		t.Fatalf("a declared action was rejected: %v", err)
	}
	if dirs[0].Component != "plan_cards" {
		t.Fatalf("parsed %+v, want plan_cards", dirs[0])
	}
}

// TestDeclaredActionFieldMismatchIsRejected pins the field contract: a wrong
// method or an undeclared field key is refused, because the handler would
// otherwise read a value the declaration never exposed as model-writable.
func TestDeclaredActionFieldMismatchIsRejected(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"wrong method": `{"components":[{"type":"plan_cards","props":{"plans":[
			{"title":"x","action":{"label":"Apply","method":"GET","url":"/api/todos","fields":{"title":"x"}}}]}}]}`,
		"extra field": `{"components":[{"type":"plan_cards","props":{"plans":[` +
			`{"title":"x","action":{"label":"Apply","method":"POST",` +
			`"url":"/api/todos","fields":{"title":"x","visibility":"public"}}}]}}]}`,
		"missing field": `{"components":[{"type":"plan_cards","props":{"plans":[
			{"title":"x","action":{"label":"Apply","method":"POST","url":"/api/todos","fields":{}}}]}}]}`,
	} {
		if _, err := ParseDirectives(body); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// TestActionButtonsAreDeclaredGated pins that the gate is not plan_cards-only:
// a standalone button row is the same capability, so it gets the same check.
func TestActionButtonsAreDeclaredGated(t *testing.T) {
	t.Parallel()
	body := `{"components":[{"type":"action_buttons","props":{"actions":[
		{"label":"Do it","method":"POST","url":"/api/undeclared","fields":{}}]}}]}`
	if _, err := ParseDirectives(body); err == nil {
		t.Fatal("action_buttons accepted an undeclared action — the gate is bypassable")
	}
}

// TestPlanWithoutActionRendersNoButton pins the other half of making the action
// a prop: a plan nobody can act on shows no control at all, instead of a button
// aimed at a page the user is not on.
func TestPlanWithoutActionRendersNoButton(t *testing.T) {
	t.Parallel()
	dirs, err := ParseDirectives(`{"components":[{"type":"plan_cards","props":{"plans":[{"title":"Just read it"}]}}]}`)
	if err != nil {
		t.Fatalf("actionless plan rejected: %v", err)
	}
	comps, err := RenderAll(dirs)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var b strings.Builder
	if rerr := comps[0].Render(t.Context(), &b); rerr != nil {
		t.Fatalf("render: %v", rerr)
	}
	if strings.Contains(b.String(), "btn-primary") {
		t.Errorf("an actionless plan rendered a submit button:\n%s", b.String())
	}
	_ = templ.Component(nil)
}

// TestDeclaredActionsAreSortedForAStablePrompt pins that the advertized list
// does not reshuffle between boots: a vocabulary that changes order every start
// reads as the available actions changing.
func TestDeclaredActionsAreSortedForAStablePrompt(t *testing.T) {
	t.Parallel()
	specs := DeclaredActions()
	for i := 1; i < len(specs); i++ {
		if specs[i-1].URL > specs[i].URL {
			t.Fatalf("DeclaredActions not sorted: %q before %q", specs[i-1].URL, specs[i].URL)
		}
	}
}
