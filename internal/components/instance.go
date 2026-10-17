// SCOPE:plugin - Per-instance signal naming for Datastar components that can
// legitimately appear more than once on a page.
//
// WHY THIS EXISTS. The vendored Datastar core has exactly ONE global signal
// store addressed by absolute dot paths, and `data-signals` never receives the
// element it sits on: a nested `data-signals` MERGES into that single store
// (later write wins) and does NOT shadow for its subtree. `data-scope-children`
// is dispatched by the core morph but consumed only by Rocket, which is a
// separate beta bundle we do not load. So there is no per-instance scoping
// available, and two instances of a component that uses generic names
// ($filter, $loading, $newTitle) would drive each other.
//
// The fix is naming, and it has to be decided in ONE place per component:
// a name built at a call site is a name that will eventually be spelled two
// ways. This file is that one place — the generalized form of the toast
// precedent, which solved the same problem for stacked toasts.
//
// TWO FUNCTIONS, and no more: Signal and Signals are what the toast uses today.
// Instance-id minting and `$`-prefixed refs are deliberately NOT here — nothing
// in production calls them (measured with deadcode), and this repo does not
// carry speculative API for a component that may never exist. Add one when a
// component needs it, following the two rules below.
//
// THE TWO HARD RULES (both measured against the vendored bundle):
//
//  1. NEVER share an `id` between instances. A Datastar patch with no selector
//     resolves the target via getElementById — FIRST match only — while a patch
//     WITH a selector hits EVERY match (querySelectorAll). So a duplicated
//     `id="todo-list"` either updates one instance silently or morphs all of
//     them, depending on how the server patched. Duplicate ids also break
//     HTML semantics (data-ref, <label for>, fragment links). A component that
//     owns a patch target must derive its id from its instance key.
//
//  2. A reference that misses its prefix fails SILENTLY. `$filter` in a
//     per-instance component does not error — it reads/writes the global
//     `filter`, which reads as cross-talk between instances rather than as a
//     bug. Nothing in the runtime can catch this, which is why every name the
//     markup uses comes from Signal/Signals here.
package components

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// signalPrefix is the stem every per-instance signal name shares. Kept short
// because it appears in every expression on the element.
const signalPrefix = "g_"

// instanceLabelRe keeps what is safe in a CSS id selector and in a signal
// name: a Datastar signal path cannot contain `__` (the modifier delimiter),
// and an id has to survive `querySelector("#" + id)`. Anything else is
// replaced rather than rejected so a caller passing a doc id or a UUID with
// hyphens still gets a usable, collision-free name.
var instanceLabelRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// sanitizeInstance reduces a label to characters that are safe in both an id
// and a signal path, WITHOUT letting two different keys collapse onto one name.
//
// Collapsing is the subtle failure: `"a b"` and `"a/b"` both coarse-replace to
// `a_b`, so two distinct rows would share state — the exact bug this helper
// exists to prevent, reintroduced by sanitization. (A test caught it.) So a
// replacement records itself: if the label changed, an 8-hex-char digest of the
// ORIGINAL is appended, which keeps the readable prefix for a mnemonic key and
// makes a collision require a hash collision rather than a coincidence of
// punctuation.
func sanitizeInstance(s string) string {
	s = strings.ReplaceAll(s, "__", "_")
	safe := instanceLabelRe.ReplaceAllString(s, "_")
	safe = strings.Trim(safe, "_")
	if safe == "" {
		// Nothing survived (all punctuation, or empty): the digest is the whole
		// name, so distinct garbage stays distinct.
		return digest(s)
	}
	if safe == s {
		return safe
	}
	return safe + "_" + digest(s)
}

// digest is a short, collision-resistant fingerprint of a sanitized-away label.
// Truncated to 8 hex chars (32 bits): the domain is one page's worth of
// instances (tens, not millions), and a longer suffix would make every id
// unreadable in a patch log.
func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

// Signal returns the full dotted signal name for one signal of one instance:
// Signal("toast", "t1", "vis") → "g_toast_t1.vis".
//
// Dot-nesting (rather than a flat "g_toast_t1_vis") keeps a component's signals
// grouped in `data-json-signals` output and in the request payload, and makes
// the instance boundary visible in a debug dump. Datastar resolves dot-notation
// paths against the one global store, which is exactly what this leverages:
// nesting here is data depth, not DOM scoping.
//
// `key` must be STABLE per instance (a row id, a toast id): a key regenerated
// at render time would yield a different name on the next patch, orphaning the
// signal.
func Signal(component, key, name string) string {
	return signalPrefix + sanitizeInstance(component) + "_" + sanitizeInstance(key) + "." + name
}

// Signals keys a props map under the instance path, so `data-signals` declares
// the whole group at once:
//
//	data-signals={ SafeJSON(Signals("toast", toastID, map[string]any{"vis": false})) }
//	// → {"g_toast_<id>.vis":false}
//
// `data-signals` merges a JSON object by dot path, so a nested object would
// also work; the flat keys are used because that is what the existing payloads
// look like and because a flat map is what a request-body inspection shows.
func Signals(component, key string, kv map[string]any) map[string]any {
	out := make(map[string]any, len(kv))
	for name, v := range kv {
		out[Signal(component, key, name)] = v
	}
	return out
}
