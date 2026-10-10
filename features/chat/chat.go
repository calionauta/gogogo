// SCOPE:layer=feature,removal=feature — chat conversation model + answer parsing
package chat

import (
	"context"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/features/genui"
)

// Message is one persisted chat record: user prompt or assistant answer.
// Components holds validated catalog directives (never raw model HTML),
// so history re-renders through the same renderers as live answers.
type Message struct {
	Owner      string
	Role       string
	Text       string
	Components []genui.Directive
	IdemKey    string
	Created    time.Time
}

// MessageStore is the persistence seam: production wires PocketBase,
// tests wire the in-memory stub. Append is idempotent on
// (owner, idemKey); List returns created order for one owner only.
type MessageStore interface {
	Append(ctx context.Context, owner string, m Message) error
	List(ctx context.Context, owner string) ([]Message, error)
}

// Responder is the model leg: production passes an llm adapter, tests
// pass a stub. Same shape as the genui plugin — one prompt in, raw
// model text out, parsing stays in ParseChatAnswer.
type Responder interface {
	Respond(ctx context.Context, prompt string) (string, error)
}

// MessageView is one rendered bubble: user text or assistant text plus
// nested generative components inside the assistant bubble.
type MessageView struct {
	Role       string
	Text       string
	Components []templ.Component
}

// proseCap bounds one fallback bubble: model prose past the cap
// truncates with an ellipsis instead of breaking layout.
const proseCap = 2000

// ParseChatAnswer prefers catalog directives and degrades to prose.
// Valid envelope with registered components renders as components;
// anything else (prose, unknown types, bad props, markup, empty)
// becomes one safe text_note bubble. Never errors — chat degrades,
// it does not fail per message (unlike the Ask demo's fail-closed
// showcase, where strictness is the point being demonstrated).
func ParseChatAnswer(raw string) ([]genui.Directive, bool, error) {
	if dirs, err := genui.ParseDirectives(raw); err == nil {
		return dirs, false, nil
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		text = "…"
	}
	if len(text) > proseCap {
		text = text[:proseCap] + "…"
	}
	return []genui.Directive{{
		Component: "text_note",
		Props:     map[string]any{"text": text},
	}}, true, nil
}
