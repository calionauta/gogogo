// LLM responder legs (real/err)
package components

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/prompts"
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
	// leg has no data to fetch.
	WithOwner(owner string) Responder
}

// dataContextBlock renders the prompt section describing the viewer's own
// data. It is appended when a summarizable panel is installed, and it is
// deliberately CONDITIONAL: an unconditional "here are the real rows, use them"
// makes the model weave the user's todos into every answer, even a "hi" that
// has nothing to do with them (live report). The block stays in the prompt --
// the anti-fabrication rule needs something true to point at -- but it orders
// the model to ignore the data unless the request is about the user's own items.
//
// The render goes through the caller's composer, so the trace names
// `genui/data-context` when an answer may draw on real rows.
func dataContextBlock(ctx context.Context, composed *prompts.Composer, owner string) string {
	summary, err := DataContext(ctx, owner)
	if err != nil || strings.TrimSpace(summary) == "" {
		return ""
	}
	return composed.MustRender("genui/data-context", map[string]string{"summary": summary})
}

// logPromptTrace records the prompt SET that produced a model response: file id
// and version for every fragment that participated. That pair is the handle an
// evaluation groups by (`gen_ai.prompt.name` / `.version` once an OTel export
// path exists); the composed TEXT is never logged, because it can carry the
// user's own data.
func logPromptTrace(ctx context.Context, parts []prompts.Meta) {
	if len(parts) == 0 {
		return
	}
	ids := make([]string, 0, len(parts))
	versions := make([]string, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.ID)
		versions = append(versions, fmt.Sprintf("%s@%d", p.ID, p.Version))
	}
	observability.Info(ctx, "genui: prompt",
		append(observability.WithComponent("genui", "feature"),
			"prompt.parts", strings.Join(ids, ","),
			"prompt.parts.versioned", strings.Join(versions, ","),
		)...)
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
	composed := r.compose(ctx, prompt)
	logPromptTrace(ctx, composed.Parts())
	return r.client.Chat(ctx, composed.String())
}

// composePrompt is the prompt this leg sends, as one named function.
//
// Extracted so the WIRING can be tested without a live client: the failure it
// guards is a missing concatenation here, and a test that had to go through the
// network (or assert on source text) would not catch it. Respond is now the
// transport call and nothing else.
func (r llmResponder) composePrompt(ctx context.Context, prompt string) string {
	return r.compose(ctx, prompt).String()
}

// compose is composePrompt as a Composer, so the request that goes to the model
// carries its own provenance. The owner's data block is a fragment like any
// other: it participates, and `genui/data-context` is one of the files a trace
// must name when an answer drew on the user's rows.
func (r llmResponder) compose(ctx context.Context, prompt string) *prompts.Composer {
	composed := ComposeConversation(ctx, r.router, prompt, r.surfaces)
	dataContextBlock(ctx, composed, r.owner)
	return composed
}

// WithOwner implements OwnerContext.
func (r llmResponder) WithOwner(owner string) Responder {
	r.owner = owner
	return r
}

type errResponder struct{ err error }

func (e errResponder) Respond(_ context.Context, _ string) (string, error) {
	return "", e.err
}

// WithOwner implements OwnerContext as a no-op: this leg fails on every
// request, so scoping changes nothing — but it must satisfy the interface so the
// worker's dispatch is uniform rather than conditional on the leg's health.
func (e errResponder) WithOwner(string) Responder { return e }

// NewResponder picks the model leg: a real key answers, and nothing else does.
//
// The simulated client used to get a canned "demo answer" — one fixed grid for
// every prompt. It made the page look configured while ignoring what the user
// asked, which is worse than an honest refusal: the answer was wrong, not
// absent, and nothing said so. A surface without a model now renders the
// shared setup notice instead (see internal/components.LLMRequired), and this
// leg fails closed with the knob named.
//
// router is optional (nil = off) and surfaces is the enabled answer vocabulary
// (nil = Chat defaults).
func NewResponder(real *llm.Client, router Router, surfaces []Surface) Responder {
	if real != nil && real.Configured() {
		return llmResponder{client: real, router: router, surfaces: surfaces, chat: true}
	}
	return errResponder{err: errors.New("genui: no LLM configured (set GOAI_API_KEY)")}
}
