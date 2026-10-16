// SCOPE:feature — chat conversation model + answer parsing
package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/features/genui/components"
)

// Message is one persisted chat record: user prompt or assistant answer.
// Components holds validated catalog directives (never raw model HTML),
// so history re-renders through the same renderers as live answers.
type Message struct {
	Owner      string
	Role       string
	Text       string
	Components []components.Directive
	IdemKey    string
	Created    time.Time
}

// MessageStore is the persistence seam: production currently wires the
// in-memory MemStore (history is volatile — lost on restart), tests wire
// the in-memory stub. Append is idempotent on
// (owner, idemKey); List returns created order for one owner only.
//
// The seam stays an interface with one production implementation on
// purpose: chat history is the next Turso-backed store (owner-scoped rows
// via dbgen, same shape as the todo slice), and the swap must not touch
// the handler or the worker. See MemStore for the documented target.
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

// unrenderable is the fallback bubble for an answer whose payload failed
// validation with no prose left around it. The payload itself is never
// shown: a chat bubble holding a wall of JSON is the failure this
// replaces, and "could not be rendered" is at least readable.
const unrenderable = "The model's answer could not be rendered."

// ParseChatAnswer prefers catalog directives and degrades to prose.
// Valid envelope with registered components renders as components;
// anything else (prose, unknown types, bad props, markup, empty)
// becomes one safe text_note bubble carrying the model's PROSE, with the
// payload it failed on stripped out. Never errors — chat degrades,
// it does not fail per message (unlike the Ask demo's fail-closed
// showcase, where strictness is the point being demonstrated).
func ParseChatAnswer(raw string) ([]components.Directive, bool, error) {
	if dirs, err := components.ParseDirectives(raw); err == nil {
		return dirs, false, nil
	}
	text := stripPayload(raw)
	if text == "" {
		text = unrenderable
	}
	if len(text) > proseCap {
		text = text[:proseCap] + "…"
	}
	return []components.Directive{{
		Component: "text_note",
		Props:     map[string]any{"text": text},
	}}, true, nil
}

// ParseChatAnswerForRepair is ParseChatAnswer with the repair policy on
// top. Chat supports TWO answer shapes — a catalog envelope and prose —
// so both validate here, and only the shape a repair can actually fix is
// reported as a failure: a payload that failed validation with no prose
// around it (the `unrenderable` bubble). Without this gate the repair
// loop would spend a round-trip nudging a conversational prose answer
// into JSON, making the bubble worse at the cost of a model call.
func ParseChatAnswerForRepair(raw string) ([]components.Directive, error) {
	dirs, fallback, err := ParseChatAnswer(raw)
	if err != nil {
		return nil, err
	}
	if fallback && isUnrenderable(dirs) {
		return nil, errors.New("chat: answer was a payload with no prose to keep")
	}
	return dirs, nil
}

// isUnrenderable reports whether the fallback bubble carries nothing but
// the "could not be rendered" placeholder, i.e. the answer was a payload
// and nothing else.
func isUnrenderable(dirs []components.Directive) bool {
	if len(dirs) != 1 || dirs[0].Component != "text_note" {
		return false
	}
	text, _ := dirs[0].Props["text"].(string)
	return text == unrenderable
}

// stripPayload removes the JSON payload a model answer was built around
// — fenced or bare — and keeps the prose written around it. The fallback
// bubble is for reading, and JSON is not reading: printing the envelope
// is exactly what a live run showed the user, so a payload that failed
// the catalog leaves the conversation as prose alone. Prose on both sides
// of the block survives; an unterminated fence means the payload ran on,
// so nothing after it is prose.
func stripPayload(raw string) string {
	var b strings.Builder
	rest := raw
	for {
		open := strings.Index(rest, "```")
		if open < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:open])
		rest = rest[open+3:]
		closed := strings.Index(rest, "```")
		if closed < 0 {
			break
		}
		rest = rest[closed+3:]
	}
	text := b.String()
	if start, end := strings.IndexByte(text, '{'), strings.LastIndexByte(text, '}'); start >= 0 && end > start {
		text = text[:start] + text[end+1:]
	}
	return strings.TrimSpace(text)
}
