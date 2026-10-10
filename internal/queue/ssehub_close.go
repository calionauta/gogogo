// SCOPE:layer=infra,removal=core — SSE Hub lifecycle (Close for bounded shutdown)
package queue

// Close drops all live registrations and replay buffers. Sends after
// Close re-buffer for a future client; existing ServeStream loops exit
// via their request context, so Close never blocks producers.
//
// Called from Queue.Close after WorkerPool.Stop so shutdown is bounded:
// workers stop producing, live tabs disconnect via HTTP server shutdown,
// and the hub holds no stale channels.
func (h *SSEHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.clients)
	clear(h.buffer)
	clear(h.userOf)
}
