// SCOPE:plugin — LLM responder legs (real/simulated/demo/err)
package components

import (
	"context"
	"errors"

	"github.com/calionauta/gogogo/internal/llm"
)

// Responder is the model leg behind Ask: production passes an llm
// adapter, tests pass a stub. The queue worker — not the HTTP handler
// calls it, because an LLM round-trip never runs inline (>50ms rule).
type Responder interface {
	Respond(ctx context.Context, prompt string) (string, error)
}

// llmResponder asks a real model for catalog directives.
type llmResponder struct {
	client *llm.Client
}

func (r llmResponder) Respond(ctx context.Context, prompt string) (string, error) {
	return r.client.Chat(ctx, PromptForCatalog()+"\nUser request: "+prompt)
}

// chatLLMResponder asks a real model with the chat affordance: the
// structured catalog plus the invented-visual html_block path.
type chatLLMResponder struct {
	client *llm.Client
}

func (r chatLLMResponder) Respond(ctx context.Context, prompt string) (string, error) {
	return r.client.Chat(ctx, PromptForChat()+"\nUser request: "+prompt)
}

// demoResponder is the keyless showcase: deterministic directives
// exercising every catalog component, labeled as demo data. Same
// role as SIMULATE_LLM in todo — the feature works with zero keys.
type demoResponder struct{}

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

func (demoResponder) Respond(_ context.Context, _ string) (string, error) {
	return demoAnswer, nil
}

type errResponder struct{ err error }

func (e errResponder) Respond(_ context.Context, _ string) (string, error) {
	return "", e.err
}

// NewResponder picks the model leg: real key wins, simulated key gets
// the deterministic demo, neither is a fail-closed error (the worker
// releases the spinner through the error result either way).
func NewResponder(real, simulated *llm.Client) Responder {
	if real != nil && real.Configured() {
		return llmResponder{client: real}
	}
	if simulated != nil && simulated.Configured() {
		return demoResponder{}
	}
	return errResponder{err: errors.New("genui: no LLM configured (GOAI_API_KEY or SIMULATE_LLM=true)")}
}

// NewChatResponder is the chat leg: same fallback shape as NewResponder,
// but the real model gets the chat affordance (catalog + invented-visual
// html_block) instead of the catalog-only Ask prompt.
func NewChatResponder(real, simulated *llm.Client) Responder {
	if real != nil && real.Configured() {
		return chatLLMResponder{client: real}
	}
	if simulated != nil && simulated.Configured() {
		return demoResponder{}
	}
	return errResponder{err: errors.New("genui: no LLM configured (GOAI_API_KEY or SIMULATE_LLM=true)")}
}
