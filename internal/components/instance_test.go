// SCOPE:plugin - Per-instance signal naming helper: collision + safety.
//
// The helper exposes Signal and Signals (what the toast uses); the tests here
// pin the two properties the naming scheme has to have — two instances never
// collide, and a hostile key still yields a usable, distinct name.
//
// An earlier version of this file also tested InstanceID/Ref/QueryID, which
// were removed as speculative API (no production caller — measured with
// deadcode). If a component ever needs a per-instance DOM id, its test belongs
// back here with it.
package components_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/components"
)

// TestSignalNamesAreGroupedAndDistinct checks the three properties the markup
// depends on: the dotted group is per instance, two instances never collide,
// and the name contains no modifier delimiter for Datastar to misread.
func TestSignalNamesAreGroupedAndDistinct(t *testing.T) {
	t.Parallel()
	a := components.Signal("toast", "t1", "vis")
	b := components.Signal("toast", "t2", "vis")
	if a == b {
		t.Fatalf("two instances share a signal name: %q", a)
	}
	if a != "g_toast_t1.vis" {
		t.Errorf("Signal = %q, want g_<component>_<key>.<name>", a)
	}
	// The dotted path must not contain `__`, which Datastar parses as a
	// modifier rather than as part of the name.
	if strings.Contains(a, "__") {
		t.Errorf("signal name %q contains `__`, which Datastar reads as a modifier", a)
	}
	// Different component names never collide either.
	if components.Signal("toast", "x", "vis") == components.Signal("dialog", "x", "vis") {
		t.Error("two components share a signal name for the same key")
	}
}

// TestSignalNamesSurviveHostileKeys pins sanitization: keys come from row ids,
// message ids and query params, so `__` (a modifier delimiter), dots, spaces
// and slashes all have to be neutralized rather than producing a broken
// selector or an unintended nested path.
func TestSignalNamesSurviveHostileKeys(t *testing.T) {
	t.Parallel()
	for _, key := range []string{
		"a__b", "has space", "with/slash", "dot.ted", "quo'te", `dq"uote`,
		"<script>", "café", "..", "", "  ", "-",
	} {
		name := components.Signal("toast", key, "vis")
		if name == "" {
			t.Errorf("key %q produced an empty name", key)
		}
		if strings.Contains(name, "__") {
			t.Errorf("key %q produced a name with the modifier delimiter: %q", key, name)
		}
		if strings.Contains(name, "..") {
			t.Errorf("key %q produced an empty path segment: %q", key, name)
		}
		if !strings.HasSuffix(name, ".vis") {
			t.Errorf("key %q lost the signal name suffix: %q", key, name)
		}
	}
	// Two DIFFERENT hostile keys must not collapse onto one name — the failure
	// a coarse replace would introduce (both of these look like "a_b").
	if components.Signal("toast", "a b", "vis") == components.Signal("toast", "a/b", "vis") {
		t.Error("distinct keys collapsed onto one signal name")
	}
	// And the sanitization is deterministic: the same key always names the same
	// signal, or a later patch would address a different one. Two separate calls
	// (not one call compared to itself, which staticcheck correctly rejects as a
	// tautology).
	first := components.Signal("toast", "a b", "vis")
	second := components.Signal("toast", "a b", "vis")
	if first != second {
		t.Errorf("Signal is not deterministic: %q then %q", first, second)
	}
}

// resolveByPath walks a decoded JSON payload the way Datastar resolves a signal
// path: one map level per dot segment. A flat key that merely CONTAINS a dot
// does not match — which is precisely the bug this guards against.
func resolveByPath(t *testing.T, payload map[string]any, path string) (any, bool) {
	t.Helper()
	var node any = payload
	for part := range strings.SplitSeq(path, ".") {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		node, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return node, true
}

// TestSignalsDeclaresTheInstanceAsANestedObject pins the data-signals payload
// against the way Datastar actually reads it. A flat {"g_toast_t1.vis":false}
// declared a signal named with a literal dot, while `$g_toast_t1.vis` read a
// different, empty signal — the toast never opened (opacity 0 for its whole
// life) and every assertion on "the toast rendered" still passed.
func TestSignalsDeclaresTheInstanceAsANestedObject(t *testing.T) {
	t.Parallel()
	got := components.Signals("toast", "t1", map[string]any{"vis": false, "count": 2})
	if len(got) != 1 {
		t.Fatalf("Signals returned %d top-level keys, want 1 (the instance object): %v", len(got), got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal signals: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode signals: %v", err)
	}
	for path, want := range map[string]any{
		"g_toast_t1.vis":   false,
		"g_toast_t1.count": float64(2),
	} {
		if v, ok := resolveByPath(t, decoded, path); !ok || v != want {
			t.Errorf("signal %s does not resolve to %v (got %v, present=%v) — payload %s",
				path, want, v, ok, encoded)
		}
	}
}

// TestSignalsFeedsARealExpression is a guard against the silent failure mode
// described in the helper's file comment: it builds the exact strings a
// template would, so a change in the naming shape shows up here as a diff
// rather than as an invisible toast in a browser.
func TestSignalsFeedsARealExpression(t *testing.T) {
	t.Parallel()
	payload := components.Signals("toast", "t9", map[string]any{"vis": false})
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal signals: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode signals: %v", err)
	}
	// The expression form a template writes (toast.templ does exactly this).
	expr := "$" + components.Signal("toast", "t9", "vis")
	if expr != "$g_toast_t9.vis" {
		t.Fatalf("expression = %q, want $g_toast_t9.vis", expr)
	}
	if _, ok := resolveByPath(t, decoded, strings.TrimPrefix(expr, "$")); !ok {
		t.Errorf("the template's expression %s does not resolve against its own payload %s", expr, encoded)
	}
}
