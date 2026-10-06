// SCOPE:layer=infra,removal=core — SSE Hub backpressure/drop-reason contract
package queue

import (
	"sync"
	"testing"
)

// TestSSEHub_EveryDropReasonIsReported pins the backpressure contract at every
// fan-out site, not just Send.
//
// This exists because the four "deliver or drop" sites (the replay drain in
// Register, Broadcast, BroadcastExcept, BroadcastToUser) shared one copied
// select, so nothing verified that each still reported its OWN reason — the
// reason strings were the only thing distinguishing them. They now route
// through deliverOrDrop; this asserts the policy per path, so a future change
// that silently makes one path block (or mislabel a drop) fails here.
func TestSSEHub_EveryDropReasonIsReported(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		reason   string
		separate bool // true => a second slow client must NOT be dropped
		run      func(h *SSEHub, slow chan []byte)
	}{
		{
			name:   "Broadcast drops a full client",
			reason: "slow-client-broadcast",
			run:    func(h *SSEHub, _ chan []byte) { h.Broadcast([]byte("x")) },
		},
		{
			name:   "BroadcastExcept drops a non-excluded full client",
			reason: "slow-client-broadcast",
			run: func(h *SSEHub, _ chan []byte) {
				h.BroadcastExcept([]byte("x"), "someone-else")
			},
		},
		{
			name:   "BroadcastToUser drops a full client of that user",
			reason: "slow-client-broadcast-user",
			run:    func(h *SSEHub, _ chan []byte) { h.BroadcastToUser([]byte("x"), "u1", "other") },
		},
		{
			name:   "replay drain drops a full channel on re-register",
			reason: "slow-client-replay",
			run: func(h *SSEHub, slow chan []byte) {
				// Buffer an event while the client is NOT registered, then register
				// a channel with no room so the synchronous drain must drop it.
				//
				// Order matters: Send to an UNREGISTERED id buffers without
				// reporting a drop. Sending first and registering second is the
				// only way to reach the drain — if the client were already
				// registered, Send would report "slow-client" and buffer
				// afterwards, which is a different path.
				h.Send("not-yet-here", []byte("buffered"))
				h.Register("not-yet-here", "u1", slow)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var mu sync.Mutex
			var reasons []string
			h := NewSSEHub(WithDropHandler(func(_ string, _ []byte, reason string) {
				mu.Lock()
				reasons = append(reasons, reason)
				mu.Unlock()
			}))

			// A full, unbuffered-into client: capacity 0 so any send is a drop.
			slow := make(chan []byte)
			// Register under the id each case targets. The replay case registers
			// its own id inside run(), so it registers nothing here.
			if tc.reason != "slow-client-replay" {
				h.Register("c", "u1", slow)
			}

			tc.run(h, slow)

			mu.Lock()
			defer mu.Unlock()
			if len(reasons) == 0 {
				t.Fatal("expected a drop, got none — the fan-out path did not report backpressure")
			}
			for _, r := range reasons {
				if r != tc.reason {
					t.Errorf("drop reason = %q, want %q", r, tc.reason)
				}
			}
		})
	}
}

// TestSSEHub_ReplayDisabledReportsDrop asserts the maxBuffer<=0 contract:
// events are dropped and reported, never silently discarded.
func TestSSEHub_ReplayDisabledReportsDrop(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var reasons []string
	h := NewSSEHub(
		WithReplayBufferSize(0), // replay disabled
		WithDropHandler(func(_ string, _ []byte, reason string) {
			mu.Lock()
			reasons = append(reasons, reason)
			mu.Unlock()
		}),
	)

	h.Send("unregistered", []byte("x"))

	mu.Lock()
	defer mu.Unlock()
	if len(reasons) != 1 || reasons[0] != "replay-disabled" {
		t.Errorf("reasons = %v, want exactly [replay-disabled]", reasons)
	}
}
