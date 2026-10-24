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
// installed, and it is deliberately CONDITIONAL: an unconditional "here are
// the real rows, use them" makes the model weave the user's todos into every
// answer, even a "hi" that has nothing to do with them (live report). The
// block stays in the prompt -- the anti-fabrication rule needs something true
// to point at -- but it orders the model to ignore the data unless the request
// is about the user's own items.
func dataContextBlock(ctx context.Context, owner string) string {
	summary, err := DataContext(ctx, owner)
	if err != nil || strings.TrimSpace(summary) == "" {
		return ""
	}
	return "Reference data for the signed-in user below. Use it ONLY when the " +
		"request is about the user's own items (their todos, notes, records); " +
		"otherwise ignore it entirely -- do not mention, summarize, or render it. " +
		"Greetings and other smalltalk are never about the user's data:\n" + summary
}

// llmResponder asks a real model for Chat directives. The router, when one
// is wired, only decides which component the prompt leads with.
type llmResponder struct {
	client   *llm.Client
	router   Router
	surfaces []Surface
	owner    string
	chat     bool
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
	return ConversationPrompt(ctx, r.router, prompt, r.surfaces) + dataContextBlock(ctx, r.owner)
}

// WithOwner implements OwnerContext.
func (r llmResponder) WithOwner(owner string) Responder {
	r.owner = owner
	return r
}

// demoResponder is the keyless showcase: deterministic directives,
// labeled as demo data. Same role as SIMULATE_LLM in todo — the feature
// works with zero keys.
type demoResponder struct{ answer string }

// demoAnswer is the keyless Chat showcase: the invented visual the model
// composes for a request. It is sanitized and rendered exactly like a live
// answer — one class-only grid, no styles, scripts or media — and HTML
// attributes are single-quoted so the markup nests in JSON without escaping.
// Red-proof: the assertion that the demo carries an html_block fails if the
// answer is ever replaced by prose alone.
const demoAnswer = `{"components":[` +
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
// demo answer is fixed, so routing it would be a lie. surfaces is the
// enabled answer vocabulary (nil = Chat defaults).
func NewResponder(real, simulated *llm.Client, router Router, surfaces []Surface) Responder {
	if real != nil && real.Configured() {
		return llmResponder{client: real, router: router, surfaces: surfaces, chat: true}
	}
	if simulated != nil && simulated.Configured() {
		return demoResponder{answer: demoAnswer}
	}
	return errResponder{err: errors.New("genui: no LLM configured (GOAI_API_KEY or SIMULATE_LLM=true)")}
}
