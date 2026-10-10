// SCOPE:layer=infra,removal=core — SSE Hub Close contract
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
