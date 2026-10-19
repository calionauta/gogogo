// SCOPE:core — SSE Hub Close contract
package queue

import "testing"

func TestSSEHub_CloseClearsClients(t *testing.T) {
	h := NewSSEHub()
	ch := make(chan []byte, 4)
	h.Register("c1", "u1", ch)
	h.Send("c2", []byte("buffered"))

	h.Close()

	stats := h.Stats()
	if stats.Clients != 0 {
		t.Fatalf("Clients=%d, want 0 after Close", stats.Clients)
	}
	if stats.BufferedClients != 0 || stats.BufferedEvents != 0 {
		t.Fatalf("buffers not cleared: %+v", stats)
	}
	if got := h.CountUserClients(); got != 0 {
		t.Fatalf("CountUserClients=%d, want 0 after Close", got)
	}
}

// TestSSEHub_CloseClearsViewPref pins the map Close used to miss. viewOf is
// write-only from the teardown path's point of view (nothing in Close reads
// it), so a stale entry is invisible at shutdown and only bites when the hub
// is reused and a client ID collides with a dead tab's stored filter/scope/
// search — at which point a re-list silently applies them. Asserting through
// the public ViewOf (not the map) is what makes this a contract test rather
// than an internal-state check.
func TestSSEHub_CloseClearsViewPref(t *testing.T) {
	h := NewSSEHub()
	h.SetView("c1", "active", "mine", "kestrel", "fuzzy")
	// Also arm the other per-client maps, so the assertion below would catch
	// any of them surviving too.
	h.Register("c1", "u1", make(chan []byte, 4))
	h.SetDoc("c1", "docA")

	h.Close()

	if got := h.ViewOf("c1"); got != (ViewPref{Filter: "all", Scope: "all"}) {
		t.Fatalf("ViewOf(c1)=%+v after Close, want the default (all/all) — "+
			"a stale pref would silently re-apply a dead tab's listing slice", got)
	}
}
