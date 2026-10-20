// SCOPE:core — SSE Hub lifecycle (Close for bounded shutdown)
package queue

// Close drops all live registrations and replay buffers. Sends after
// Close re-buffer for a future client; existing ServeStream loops exit
// via their request context, so Close never blocks producers.
//
// Called from Queue.Close after WorkerPool.Stop so shutdown is bounded:
// workers stop producing, live tabs disconnect via HTTP server shutdown,
// and the hub holds no stale channels.
//
// Every per-client map must be cleared here. viewOf (ViewPref, keyed by
// clientID) is the one that is easy to forget because nothing in the
// teardown path reads it: a forgotten map is invisible until the hub is
// reused and a new client ID collides with a stale preference, at which
// point a re-list silently applies the dead tab's filter/scope/search.
func (h *SSEHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ephemeral != nil {
		_ = h.ephemeral.sub.Unsubscribe()
		h.ephemeral = nil
	}
	clear(h.clients)
	clear(h.buffer)
	clear(h.userOf)
	clear(h.docOf)
	clear(h.viewOf)
}
