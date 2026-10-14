// SCOPE:feature — chat conversation model + answer parsing
package store

import (
	"context"
	"encoding/json"
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

// textPropKey is the component prose field the fallback reads and
// writes (one spelling, so miners and builders never drift).
const textPropKey = "text"

// fallbackMine caps the fallback miner: at most this many strings of
// at most this many runes each — a hostile envelope cannot blow up the
// bubble, and legit plans sit far below either bound.
const (
	fallbackMaxStrings = 12
	fallbackMaxRunes   = 280
)

// ParseChatAnswer prefers catalog directives and degrades to prose.
// Valid envelope with registered components renders as components;
// anything else (prose, unknown types, bad props, markup, empty)
// becomes one safe text_note bubble. Never errors — chat degrades,
// it does not fail per message (unlike the Ask demo's fail-closed
// showcase, where strictness is the point being demonstrated).
//
// The fallback never echoes raw model JSON: when parsing fails but the
// output looks like a component envelope (the plan-my-day report), the
// raw JSON is mined for human-readable strings (text/title/detail/
// label) instead of being dumped verbatim into the bubble.
func ParseChatAnswer(raw string) ([]components.Directive, bool, error) {
	if dirs, err := components.ParseDirectives(raw); err == nil {
		return dirs, false, nil
	}
	if text := readableFallback(raw); text != "" {
		return []components.Directive{{
			Component: "text_note",
			Props:     map[string]any{textPropKey: text},
		}}, true, nil
	}
	text := strings.TrimSpace(raw)
	if text == "" {
		text = "…"
	}
	if len(text) > proseCap {
		text = text[:proseCap] + "…"
	}
	return []components.Directive{{
		Component: "text_note",
		Props:     map[string]any{textPropKey: text},
	}}, true, nil
}

// readableFallback extracts human-readable strings from a failed
// component envelope so the fallback bubble reads as prose, not JSON.
// Returns "" when the input does not look like JSON (plain prose takes
// the normal path above).
func readableFallback(raw string) string {
	if !strings.Contains(raw, `"components"`) && !strings.Contains(raw, `"type"`) {
		return ""
	}
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return ""
	}
	got := collectOrdered(raw[start:], map[string]bool{
		textPropKey: true, "title": true, "detail": true, "label": true,
	}, fallbackMaxStrings, fallbackMaxRunes)
	if len(got) == 0 {
		return "I couldn't shape that answer as components — try asking again."
	}
	out := strings.Join(got, "\n• ")
	if len(out) > proseCap {
		out = out[:proseCap] + "…"
	}
	return out
}

// maxFallbackJSONBytes bounds the fallback miner: model output past
// this is parsed for nothing (a generic apology instead). Provider
// output caps sit far below it, so legit plans never notice.
const maxFallbackJSONBytes = 64 << 10

// maxWalkDepth bounds the streaming walk below: hostile nesting cannot
// recurse the worker's stack (same reason the catalog caps directive
// depth).
const maxWalkDepth = 32

// collectOrdered walks decoded JSON depth-first IN DOCUMENT ORDER and
// collects string values held under wanted keys (component prose
// fields). Bounded by maxN values of maxLen runes each — a hostile
// envelope cannot blow up the fallback bubble.
//
// Unlike an Unmarshal-into-any walk (Go maps iterate randomly, so
// "Morning" could print after "Evening" on some requests), this streams
// tokens, so the fallback reads in the order the model wrote it.
// Partial results survive malformed tails: whatever was collected
// before the syntax error still renders.
func collectOrdered(raw string, wanted map[string]bool, maxN, maxLen int) []string {
	if len(raw) > maxFallbackJSONBytes {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	var out []string
	_ = walkValue(dec, "", wanted, maxN, maxLen, 0, &out)
	return out
}

// walkValue consumes exactly one JSON value from dec, collecting
// strings held under wanted keys. key is the object key this value
// belongs to ("" for array elements and the top level).
func walkValue(dec *json.Decoder, key string, wanted map[string]bool, maxN, maxLen, depth int, out *[]string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		collectString(tok, key, wanted, maxN, maxLen, out)
		return nil
	}
	if depth >= maxWalkDepth {
		// Over-deep subtree: drain its tokens without recursing.
		// drainValue(1) accounts for the opener just consumed.
		return drainValue(dec, 1)
	}
	switch d {
	case '{':
		return walkObject(dec, wanted, maxN, maxLen, depth, out)
	case '[':
		return walkArray(dec, wanted, maxN, maxLen, depth, out)
	}
	return nil
}

// walkObject consumes one {...} body (opener already read): each key
// in document order, each value through walkValue.
func walkObject(dec *json.Decoder, wanted map[string]bool, maxN, maxLen, depth int, out *[]string) error {
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		ks, _ := kt.(string)
		if err := walkValue(dec, ks, wanted, maxN, maxLen, depth+1, out); err != nil {
			return err
		}
	}
	_, err := dec.Token() // consume '}'
	return err
}

// walkArray consumes one [...] body (opener already read): elements in
// document order through walkValue.
func walkArray(dec *json.Decoder, wanted map[string]bool, maxN, maxLen, depth int, out *[]string) error {
	for dec.More() {
		if err := walkValue(dec, "", wanted, maxN, maxLen, depth+1, out); err != nil {
			return err
		}
	}
	_, err := dec.Token() // consume ']'
	return err
}

// collectString keeps one scalar token when it is a wanted prose field.
// Anything else (numbers, bools, null, uninteresting keys) is ignored.
func collectString(tok any, key string, wanted map[string]bool, maxN, maxLen int, out *[]string) {
	s, ok := tok.(string)
	if !ok || !wanted[key] || len(*out) >= maxN {
		return
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	*out = append(*out, s)
}

// drainValue consumes tokens until the container opened before it
// closes (level counts the already-consumed opener). Only reached on
// the over-deep path above; malformed input ends the drain as an error
// the caller already tolerates (partial results still render).
func drainValue(dec *json.Decoder, level int) error {
	for level > 0 {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				level++
			case '}', ']':
				level--
			}
		}
	}
	return nil
}
