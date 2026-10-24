// SCOPE:feature — chat conversation model + answer parsing
package store

import (
	"context"
	"errors"
	"fmt"
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
// pass a stub. Same shape as the generative catalog — one prompt in, raw
// model text out, parsing stays in ParseChatAnswer.
type Responder interface {
	Respond(ctx context.Context, prompt string) (string, error)
}

// OwnerContext is the optional seam a responder implements when it can scope
// itself to one account, so the leg can put the user's REAL data in the prompt
// instead of letting the model invent it. The generative package owns the
// implementation; declared here because this package consumes it (owning the
// interface at the consumer is what keeps chat from depending on genui's
// internals).
type OwnerContext interface {
	WithOwner(owner string) Responder
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

// textNoteComponent and diagramComponent name the fallback directives
// ParseChatAnswer mints without going through the catalog parser. Named
// once so the three construction sites cannot drift.
const (
	textNoteComponent = "text_note"
	diagramComponent  = "diagram"
	// propText is the text_note payload key, shared by the fallback bubbles
	// the worker, and the history views that read them back.
	propText = "text"
)

// unrenderable is the fallback bubble for an answer whose payload failed
// validation with no prose left around it. The raw payload is never shown (a
// chat bubble holding a wall of JSON is the failure this replaces), but the
// bubble must still be ANSWERABLE rather than cryptic: the previous text
// ("The model's answer could not be rendered.") told the user nothing about
// what to do, read as an app error, and was reported as a bug — the request had
// in fact been understood, and rephrasing usually gets a real answer.
//
// What is deliberately NOT said: which component failed or why. Those messages
// come from the catalog's validator, name internal props, and are for the log —
// the actionable part is only ever "ask again, differently".
const unrenderable = "I couldn't shape that into a visual answer. Ask again with a " +
	"bit more detail — or, if you were asking about your own data (todos, " +
	"records), the Chat panel renders it directly from the app."

// ParseChatAnswer prefers catalog directives and degrades to prose.
// Valid envelope with registered components renders as components; a
// partially valid envelope renders what validates (unknown types become a
// text_note catch-all, undecodable props are dropped and counted, surviving
// prose rides along); a ```mermaid fence anywhere renders as a diagram even
// when the envelope failed; anything else becomes one safe text_note bubble
// carrying the model's PROSE, with the payload it failed on stripped out.
// Never errors — chat degrades, it does not fail per message: a
// conversation that errors per message is unusable, so degradation is the
// contract.
//
// dropped counts directives the lenient pass could not render, for the
// worker's outcome log (repair rate per component is the number that says
// whether the prompt or the catalog is the thing to fix).
func ParseChatAnswer(raw string) ([]components.Directive, int, bool, error) {
	dirs, failures, fallback := resolveChatAnswer(raw)
	return dirs, components.DroppedCount(failures), fallback, nil
}

// resolveChatAnswer runs the full Chat pipeline once: strict, then lenient
// with prose, then fences, then the prose fallback. ParseChatAnswer and
// ParseChatAnswerForRepair are two views over it (counts vs repair), so the
// render and the retry can never disagree about what the answer held.
func resolveChatAnswer(raw string) (dirs []components.Directive, failures []error, fallback bool) {
	if dirs, err := components.ParseDirectives(raw); err == nil {
		return dirs, nil, false
	}
	if dirs, errs := components.ParseLenient(raw); len(dirs) > 0 {
		merged := prependProse(dirs, raw)
		merged = appendFenceDiagrams(merged, components.ExtractMermaidFences(raw))
		return merged, errs, false
	} else if len(errs) > 0 {
		failures = errs
	}
	if fences := components.ExtractMermaidFences(raw); len(fences) > 0 {
		return fenceDiagrams(fences, raw), failures, false
	}
	return proseFallback(raw), failures, true
}

// prependProse keeps the model's writing around a partially valid envelope:
// the JSON payload is for reading by the parser, the prose is for reading
// by the user, and dropping it turned a half-good answer into components
// with no lead-in.
func prependProse(dirs []components.Directive, raw string) []components.Directive {
	text := stripPayload(raw)
	if text == "" {
		return dirs
	}
	if len(text) > proseCap {
		text = text[:proseCap] + "…"
	}
	return append([]components.Directive{{
		Component: textNoteComponent,
		Props:     map[string]any{propText: text},
	}}, dirs...)
}

// fenceDiagrams builds diagram directives from ```mermaid fences plus the
// surrounding prose, for answers with no usable envelope at all.
func fenceDiagrams(fences []string, raw string) []components.Directive {
	var dirs []components.Directive
	if text := stripPayload(raw); text != "" {
		if len(text) > proseCap {
			text = text[:proseCap] + "…"
		}
		dirs = append(dirs, components.Directive{
			Component: textNoteComponent,
			Props:     map[string]any{propText: text},
		})
	}
	for _, src := range fences {
		dirs = append(dirs, components.Directive{
			Component: diagramComponent,
			Props:     map[string]any{"source": src},
		})
	}
	return dirs
}

// proseFallback is the last resort: one safe text_note bubble carrying the
// model's prose (or the unrenderable placeholder when there is no prose).
func proseFallback(raw string) []components.Directive {
	text := stripPayload(raw)
	if text == "" {
		text = unrenderable
	}
	if len(text) > proseCap {
		text = text[:proseCap] + "…"
	}
	return []components.Directive{{
		Component: textNoteComponent,
		Props:     map[string]any{propText: text},
	}}
}

// appendFenceDiagrams adds ```mermaid fences not already present as diagram
// directives: a fence the envelope already shapes (as a diagram component)
// is not duplicated, and a fence beside a broken envelope still renders.
func appendFenceDiagrams(dirs []components.Directive, fences []string) []components.Directive {
	if len(fences) == 0 {
		return dirs
	}
	seen := map[string]bool{}
	for _, d := range dirs {
		if d.Component != diagramComponent {
			continue
		}
		if src, _ := d.Props["source"].(string); src != "" {
			seen[src] = true
		}
	}
	for _, src := range fences {
		if seen[src] {
			continue
		}
		dirs = append(dirs, components.Directive{
			Component: diagramComponent,
			Props:     map[string]any{"source": src},
		})
	}
	return dirs
}

// ParseChatAnswerForRepair is resolveChatAnswer with the repair policy on
// top. Chat supports TWO answer shapes — a catalog envelope and prose —
// so both validate here, and only shapes a repair can actually fix are
// reported as failures: dropped directives (a retry with the failure fed
// back fixes the one component that broke) and a payload that failed
// validation with no prose around it (the `unrenderable` bubble). Without
// this gate the repair loop would spend a round-trip nudging a
// conversational prose answer into JSON, making the bubble worse at the
// cost of a model call.
func ParseChatAnswerForRepair(raw string) ([]components.Directive, error) {
	dirs, failures, fallback := resolveChatAnswer(raw)
	if dropped := components.DroppedCount(failures); dropped > 0 {
		return nil, firstFailure(failures, dropped)
	}
	if fallback && isUnrenderable(dirs) {
		return nil, errors.New("chat: answer was a payload with no prose to keep")
	}
	return dirs, nil
}

// firstFailure names the repair target: the first dropped component's
// error plus the count, so the model fixes the one thing that broke
// instead of re-rolling the whole answer.
func firstFailure(failures []error, dropped int) error {
	for _, err := range failures {
		if errors.Is(err, components.ErrNoEnvelope) {
			continue
		}
		if dropped > 1 {
			return fmt.Errorf("chat: %d directives failed validation; first: %w", dropped, err)
		}
		return fmt.Errorf("chat: directive failed validation: %w", err)
	}
	return fmt.Errorf("chat: %d directive(s) failed validation", dropped)
}

// isUnrenderable reports whether the fallback bubble carries nothing but
// the "could not be rendered" placeholder, i.e. the answer was a payload
// and nothing else.
func isUnrenderable(dirs []components.Directive) bool {
	if len(dirs) != 1 || dirs[0].Component != textNoteComponent {
		return false
	}
	text, _ := dirs[0].Props[propText].(string)
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

// IDPrefix derives a DOM-id stem from a stored message.
//
// Every component a message rendered needs an id unique to that message,
// because a chat thread keeps its history: two answers on one page would
// otherwise mint the same ids (invalid HTML, and a selector that means "the
// first match" — see internal/components/instance.go rule 1).
//
// Derived from IdemKey rather than a random value per render: the stem must be
// STABLE across renders, or a page that re-renders an old answer would move its
// ids and any patch already aimed at one would miss. The key is already unique
// per message and already persisted, so it is the identity this needs without
// adding a field. It is sanitized because the key arrives from the client and
// becomes part of an id.
func IDPrefix(m Message) string {
	key := m.IdemKey
	if key == "" {
		// A message stored without a key (every user echo, and any record the
		// store accepted without one) still needs a stable stem. CreatedUnixNano
		// is stable because it is stamped once, at append.
		key = fmt.Sprintf("t%d", m.Created.UnixNano())
	}
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return "chat-" + b.String()
}
