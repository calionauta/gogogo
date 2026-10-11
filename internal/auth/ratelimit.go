// SCOPE:core — In-process attempt throttling for low-entropy secrets
//
// TOTP codes have ~1M possibilities: online guessing is the attack, and
// throttling is the defense. This tracker allows a burst (10 attempts per
// 5 minutes per key) then fails closed until the window slides. Passwords
// are NOT tracked here — their space needs no online throttle, and tracking
// by email would let anyone lock anyone else out (a worse hole than the
// one it plugs); password brute force belongs at the edge (reverse proxy
// rate limits), documented, not built.
//
// Single-box scope, like the OAuth handshake states: the zero-dependency
// template default. A fleet shares nothing yet, so it shares this too.
package auth

import (
	"sync"
	"time"
)

// attemptWindow is the sliding window; attemptMax the burst it holds.
const (
	attemptWindow = 5 * time.Minute
	attemptMax    = 10
)

// attemptTracker counts recent failures per key (user id), forgetting them
// as the window slides. Zero value ready; safe for concurrent use.
type attemptTracker struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

// allow records a failure for key and reports whether the key may try
// again. Call it ONLY on failure: successes reset via forget.
func (t *attemptTracker) allow(key string) bool {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.hits == nil {
		t.hits = map[string][]time.Time{}
	}
	kept := t.hits[key][:0]
	for _, at := range t.hits[key] {
		if now.Sub(at) < attemptWindow {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now)
	t.hits[key] = kept
	return len(kept) <= attemptMax
}

// forget clears a key's history (called on success, so one typo burst
// does not poison the next login five minutes later).
func (t *attemptTracker) forget(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.hits, key)
}
