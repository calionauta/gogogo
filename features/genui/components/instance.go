// SCOPE:plugin — per-component instance ids, so a rendered answer is
// addressable and two of the same component can coexist.
//
// WHY THIS EXISTS. Every catalog leaf and every native panel rendered with a
// fixed id or no id at all: `#todo-panel` was hardcoded in the panel template,
// and the leaves (plan_cards, data_table, ...) carried no id. That is invisible
// while an answer renders each component once, and breaks the moment it does
// not — a chat thread that keeps its history renders EVERY past answer, so two
// todo panels on the page made the id non-unique (invalid HTML, and a selector
// that silently means "the first one"), and a component that wants to patch or
// extend itself has nothing to aim at.
//
// The id is assigned HERE, once, on the DIRECTIVE — not inside the components.
// That placement is the whole design: it means the value exists before render
// (so a handler can compute the same id a browser will see), it survives being
// stored as JSON on a chat message, and it costs each component one extra
// parameter instead of one more reason to disagree about where ids come from.
//
// The PROPERTY the ids must have is uniqueness per rendered page, not
// global uniqueness: they are DOM ids. Composers that append answers to one
// growing page (chat) therefore pass fresh ids per message; composers that
// replace a single region (Ask) can pass none and let the sequence below do
// the work.
package components

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// InstanceIDProp is the reserved prop key holding a directive's instance id.
//
// A leading underscore marks it as SERVER-OWNED: no component may list it in
// Writable(), and a model that sends it is ignored (see ensureInstanceID).
// Models do sometimes emit an `id`; treating that as an address would let an
// answer choose DOM ids, which is exactly the kind of control the props
// safelist exists to deny.
const InstanceIDProp = "_id"

// idPrefix namespaces generated ids so they are recognizable in a DOM
// inspector and cannot collide with the hand-written ids this app uses for
// patch targets (#todo-list, #chat-messages, #review-result).
const idPrefix = "genui-"

// maxInstanceIDLen bounds an id accepted from a caller. Ids end up in HTML
// attributes and in Datastar selectors, so an unbounded string is a way to
// smuggle a selector expression in. 64 is far more than "genui-<16 hex><-n>"
// needs and short enough to be obviously safe.
const maxInstanceIDLen = 64

// NewInstanceID returns a fresh, DOM-safe component id.
//
// Random rather than derived from the prompt or the position: a chat history
// re-renders old answers, and a position-derived id would silently change
// meaning when the thread grows (the same slot naming a different component).
// The random half is 8 bytes of crypto/rand, which is plenty — these protect
// nothing secret, they only have to not collide on one page.
func NewInstanceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is a broken platform, not a user-facing case;
		// falling back to a constant would silently duplicate ids, which is the
		// bug this function exists to prevent. Panic with the reason instead.
		panic(fmt.Sprintf("genui: instance id entropy unavailable: %v", err))
	}
	return idPrefix + hex.EncodeToString(b[:])
}

// InstanceID reads a directive's id, or "" when it has none.
func (d *Directive) InstanceID() string {
	raw, _ := d.Props[InstanceIDProp].(string)
	return raw
}

// ensureInstanceID gives a directive a valid id, keeping the caller's when it
// supplied a usable one.
//
// The id is part of the RENDER REQUEST, not of the model's answer, so a model
// that emits `_id` is overridden rather than rejected: rejecting the whole
// answer over a field it should not have sent would turn a harmless model tic
// into a failed response. Anything unsafe (not a DOM-safe identifier, too
// long) is replaced for the same reason.
func (d *Directive) ensureInstanceID(explicit string) {
	if validInstanceID(explicit) {
		if d.Props == nil {
			d.Props = map[string]any{}
		}
		d.Props[InstanceIDProp] = explicit
		return
	}
	if id := d.InstanceID(); validInstanceID(id) {
		return
	}
	if d.Props == nil {
		d.Props = map[string]any{}
	}
	d.Props[InstanceIDProp] = NewInstanceID()
}

// validInstanceID reports whether s is usable as a DOM id and as a Datastar
// selector target: non-empty, bounded, and restricted to characters that need
// no CSS escaping ([A-Za-z0-9_-] plus a leading letter). Deliberately strict —
// a permissive check would move the escaping problem into every selector.
func validInstanceID(s string) bool {
	if s == "" || len(s) > maxInstanceIDLen {
		return false
	}
	for i, r := range s {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			return false
		}
		if i == 0 && (r >= '0' && r <= '9') {
			// A leading digit makes a CSS selector invalid unquoted.
			return false
		}
	}
	return true
}
