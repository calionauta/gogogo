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

// TestSignalsKeysTheWholeGroup pins the data-signals payload: a flat map keyed
// by instance path, which is what the template hands to SafeJSON.
func TestSignalsKeysTheWholeGroup(t *testing.T) {
	t.Parallel()
	got := components.Signals("toast", "t1", map[string]any{"vis": false, "count": 2})
	if len(got) != 2 {
		t.Fatalf("Signals returned %d keys, want 2: %v", len(got), got)
	}
	if got["g_toast_t1.vis"] != false {
		t.Errorf("Signals did not key on the instance path: %v", got)
	}
	if _, ok := got["g_toast_t1.count"]; !ok {
		t.Errorf("Signals dropped a name: %v", got)
	}
}

// TestSignalsFeedsARealExpression is a guard against the silent failure mode
// described in the helper's file comment: it builds the exact strings a
// template would, so a change in the naming shape shows up here as a diff
// rather than as cross-talk between instances in a browser.
func TestSignalsFeedsARealExpression(t *testing.T) {
	t.Parallel()
	payload := components.Signals("toast", "t9", map[string]any{"vis": false})
	rendered := ""
	for k := range payload {
		rendered = k
	}
	if rendered != "g_toast_t9.vis" {
		t.Errorf("payload key = %q, want g_toast_t9.vis", rendered)
	}
	// The expression form a template writes (toast.templ does exactly this).
	expr := "$" + components.Signal("toast", "t9", "vis") + " = true"
	if expr != "$g_toast_t9.vis = true" {
		t.Errorf("expression = %q, want $g_toast_t9.vis = true", expr)
	}
}
