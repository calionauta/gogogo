// SCOPE:plugin — LLM responder legs (real/simulated/demo/err)
package components

import (
	"context"
	"errors"
	"strings"

	"github.com/calionauta/gogogo/internal/llm"
)

// Responder is the model leg behind Ask: production passes an llm
// adapter, tests pass a stub. The queue worker — not the HTTP handler
// calls it, because an LLM round-trip never runs inline (>50ms rule).
type Responder interface {
	Respond(ctx context.Context, prompt string) (string, error)
}

// OwnerContext is the optional seam that lets a responder see WHOSE data an
// answer may draw on, so it can carry a real digest of it in the prompt.
//
// A separate interface rather than a Respond parameter on purpose: the panel
// seam needs no owner to render, this needs one, and a worker that has no
// owner (or a keyless demo responder) must still compile against the same
// Respond signature. The queue worker type-asserts, exactly like
// PanelSummarizer does for providers.
type OwnerContext interface {
	// WithOwner returns a responder scoped to owner, or the receiver when the
	// leg has no data to fetch (demo/simulated).
	WithOwner(owner string) Responder
}

// dataContextBlock renders the prompt section describing the viewer's own
// data. It is prefixed to BOTH legs' prompts when a summarizable panel is
// installed, and it deliberately reads like data, not instructions: "here are
// the real rows" is what makes the fabrication rule followable.
func dataContextBlock(ctx context.Context, owner string) string {
	summary, err := DataContext(ctx, owner)
	if err != nil || strings.TrimSpace(summary) == "" {
		return ""
	}
	return "Current data from this app for the signed-in user (real, use it " +
		"instead of inventing):\n" + summary
}

// llmResponder asks a real model for catalog directives. The router, when one
// is wired, only decides which component the prompt leads with.
type llmResponder struct {
	client   *llm.Client
	router   Router
	surfaces []Surface
	owner    string
}

func (r llmResponder) Respond(ctx context.Context, prompt string) (string, error) {
	return r.client.Chat(ctx, r.composePrompt(ctx, prompt))
}

// composePrompt is the prompt this leg sends, as one named function.
//
// Extracted so the WIRING can be tested without a live client: the failure it
// guards is a missing concatenation here, and a test that had to go through the
// network (or assert on source text) would not catch it. Respond is now the
// transport call and nothing else.
func (r llmResponder) composePrompt(ctx context.Context, prompt string) string {
	return AskPrompt(ctx, r.router, prompt, r.surfaces) + dataContextBlock(ctx, r.owner)
}

// WithOwner implements OwnerContext.
func (r llmResponder) WithOwner(owner string) Responder {
	r.owner = owner
	return r
}

// chatLLMResponder asks a real model with the chat surface set (the catalog
// plus, by default, the invented-visual html_block path) and, when one is
// wired, the same router Ask uses — a conversation deserves the same decision
// about which component should lead an answer.
type chatLLMResponder struct {
	client   *llm.Client
	router   Router
	surfaces []Surface
	owner    string
}

func (r chatLLMResponder) Respond(ctx context.Context, prompt string) (string, error) {
	return r.client.Chat(ctx, r.composePrompt(ctx, prompt))
}

// composePrompt is the chat leg's counterpart to llmResponder.composePrompt,
// extracted for the same testability reason.
func (r chatLLMResponder) composePrompt(ctx context.Context, prompt string) string {
	return ConversationPrompt(ctx, r.router, prompt, r.surfaces) + dataContextBlock(ctx, r.owner)
}

// WithOwner implements OwnerContext.
func (r chatLLMResponder) WithOwner(owner string) Responder {
	r.owner = owner
	return r
}

// demoResponder is the keyless showcase: deterministic directives,
// labeled as demo data. Same role as SIMULATE_LLM in todo — the feature
// works with zero keys. The answer is a field because the two legs
// demonstrate different halves of the split: Ask shows the catalog, Chat
// shows the visual the model composes for the request (demoChatAnswer).
type demoResponder struct{ answer string }

const demoAnswer = `{"components":[` +
	`{"type":"text_note","props":{"text":"Demo answer — connect GOAI_API_KEY for live model output."}},` +
	`{"type":"bar_chart","props":{"title":"This week","bars":[` +
	`{"label":"Mon","value":3},{"label":"Tue","value":5},{"label":"Wed","value":2}]}},` +
	`{"type":"plan_cards","props":{"plans":[` +
	`{"title":"Ship the Ask panel","detail":"wire a real prompt through this same path"},` +
	`{"title":"Grow the catalog","detail":"one registry entry per new component"}]}},` +
	`{"type":"action_buttons","props":{"actions":[` +
	`{"label":"Add demo todo","method":"POST","url":"/api/todos","fields":{"title":"Demo from Ask"}}]}},` +
	`{"type":"data_table","props":{"headers":["Component","Renders"],` +
	`"rows":[["text_note","prose card"],["plan_cards","apply buttons"],["data_table","this table"]]}}]}`

// demoChatAnswer is the keyless Chat showcase: the invented-visual half of
// the split as its OWN answer, so a keyless run still shows the difference
// between the two demos (both used to answer with the catalog). It is
// sanitized and rendered exactly like a live answer — one class-only grid,
// no styles, scripts or media — and HTML attributes are single-quoted so the
// markup nests in JSON without escaping. Red-proof: render it without
// SanitizeHTML and the class-only grid still works, but the assertion that
// the chat demo carries an html_block is what fails if the two answers are
// ever collapsed back into one.
const demoChatAnswer = `{"components":[` +
	`{"type":"html_block","props":{"html":"` +
	`<div class='grid grid-cols-1 sm:grid-cols-3 gap-3'>` +
	`<div class='card bg-base-200 p-4'><p class='text-xs uppercase opacity-60'>Monday</p>` +
	`<p class='font-semibold'>Ship the smoke guard</p></div>` +
	`<div class='card bg-base-200 p-4'><p class='text-xs uppercase opacity-60'>Tuesday</p>` +
	`<p class='font-semibold'>Read the parser diff</p></div>` +
	`<div class='card bg-base-200 p-4'><p class='text-xs uppercase opacity-60'>Wednesday</p>` +
	`<p class='font-semibold'>Write the handoff note</p></div>` +
	`</div>` +
	`"}},` +
	`{"type":"text_note","props":{"text":"Demo answer — connect GOAI_API_KEY and the model ` +
	`composes this markup itself, for the request it was given."}}` +
	`]}`

func (d demoResponder) Respond(_ context.Context, _ string) (string, error) {
	return d.answer, nil
}

// WithOwner implements OwnerContext as a no-op pass-through.
//
// The keyless showcase has no store and no owner, and that is exactly why it
// implements the seam rather than letting the worker's type assertion fail: the
// assertion is the dispatch, so a responder that ignored it would silently get
// no owner context while looking like it did. Making the no-op explicit keeps
// "this leg has no data" a decision instead of an accident.
func (d demoResponder) WithOwner(string) Responder { return d }

type errResponder struct{ err error }

func (e errResponder) Respond(_ context.Context, _ string) (string, error) {
	return "", e.err
}

// WithOwner implements OwnerContext as a no-op: this leg fails on every
// request, so scoping changes nothing — but it must satisfy the interface so the
// worker's dispatch is uniform rather than conditional on the leg's health.
func (e errResponder) WithOwner(string) Responder { return e }

// NewResponder picks the model leg: real key wins, simulated key gets
// the deterministic demo, neither is a fail-closed error (the worker
// releases the spinner through the error result either way). router is
// optional (nil = off) and is consulted only for the real-model leg: the
// demo answers are fixed, so routing them would be a lie. surfaces is the
// enabled answer vocabulary (nil = the Ask defaults).
func NewResponder(real, simulated *llm.Client, router Router, surfaces []Surface) Responder {
	if real != nil && real.Configured() {
		return llmResponder{client: real, router: router, surfaces: surfaces}
	}
	if simulated != nil && simulated.Configured() {
		return demoResponder{answer: demoAnswer}
	}
	return errResponder{err: errors.New("genui: no LLM configured (GOAI_API_KEY or SIMULATE_LLM=true)")}
}

// NewChatResponder is the chat leg: same fallback shape as NewResponder,
// but the real model gets the chat surface set (invented visual first,
// catalog for what markup cannot do) instead of the Ask defaults, and the
// keyless demo answers with its own invented-visual demoChatAnswer rather
// than the catalog demo.
//
// The router is the SAME Router Ask uses (nil = off): once both legs speak
// from one surface vocabulary, deciding which component should lead is not a
// question specific to one-shot answers.
func NewChatResponder(real, simulated *llm.Client, router Router, surfaces []Surface) Responder {
	if real != nil && real.Configured() {
		return chatLLMResponder{client: real, router: router, surfaces: surfaces}
	}
	if simulated != nil && simulated.Configured() {
		return demoResponder{answer: demoChatAnswer}
	}
	return errResponder{err: errors.New("genui: no LLM configured (GOAI_API_KEY or SIMULATE_LLM=true)")}
}
