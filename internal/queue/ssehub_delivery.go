package queue

// The backpressure contract lives in its own file so the hub's registration and
// fan-out code reads as what it is: who is subscribed, and what gets published
// to whom. This is the one place that decides what happens when a subscriber is
// too slow.

// deliverOrDrop attempts a non-blocking send on ch, reporting a full channel to
// onDrop with reason. This is the backpressure contract in one place: producers
// never block on a slow client.
//
// It exists because the same select appeared at four fan-out sites — the replay
// drain in Register, Broadcast, BroadcastExcept and BroadcastToUser — differing
// only in the reason string. Four copies meant the policy could drift between
// them (one site gaining a timeout, another changing what counts as "slow")
// without any test noticing. Callers hold at least a read lock; this helper
// takes no lock of its own.
func (h *SSEHub) deliverOrDrop(clientID string, ch chan []byte, data []byte, reason string) {
	select {
	case ch <- data:
	default:
		h.onDrop(clientID, data, reason)
	}
}

// bufferEvent appends data to the per-client replay buffer, dropping
// the oldest event if the buffer is full. Caller must NOT hold the
// mutex (this function acquires it).
func (h *SSEHub) bufferEvent(clientID string, data []byte) {
	if h.maxBuffer <= 0 {
		// Replay disabled — keep the most recent event only in a
		// single-slot "in case a client connects later" buffer? No:
		// honour the contract. Drop.
		h.onDrop(clientID, data, "replay-disabled")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	buf := h.buffer[clientID]
	if len(buf) >= h.maxBuffer {
		// Ring buffer: drop oldest.
		buf = buf[1:]
	}
	h.buffer[clientID] = append(buf, data)
}
