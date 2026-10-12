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
	"net"
	"net/http"
	"sync"
	"time"
)

// attemptWindow is the sliding window; attemptMax the burst it holds.
const (
	attemptWindow = 5 * time.Minute
	attemptMax    = 10
)

// attemptTracker counts recent failures per key (user id, client IP),
// forgetting them as the window slides. Zero value ready (the package
// defaults below); safe for concurrent use. max/window override the
// defaults when nonzero — signin throttling runs a looser budget than
// TOTP throttling on the same machinery.
type attemptTracker struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	max    int
	window time.Duration
}

// signinAttemptBudget is the password-guessing budget per client IP:
// stuffing scripts do thousands per minute, offices do dozens per day.
// Per-IP (never per-email — tracking by email would let anyone lock
// anyone else out). The edge reverse proxy remains the primary throttle;
// this is defense in depth plus CPU-DoS bounding (each guess burns argon).
const signinAttemptMax = 30

// budget resolves the tracker's effective limits.
func (t *attemptTracker) budget() (max int, window time.Duration) {
	if t.max > 0 {
		max = t.max
	} else {
		max = attemptMax
	}
	if t.window > 0 {
		window = t.window
	} else {
		window = attemptWindow
	}
	return max, window
}

// allow records an attempt for key and reports whether the key may try
// again. TOTP calls it only on failure (codes are the scarce resource);
// signin calls it on entry (the argon check it gates is the scarce
// resource) and forgets on verified passwords. Either way successes reset
// via forget.
func (t *attemptTracker) allow(key string) bool {
	now := time.Now()
	max, window := t.budget()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.hits == nil {
		t.hits = map[string][]time.Time{}
	}
	kept := t.hits[key][:0]
	for _, at := range t.hits[key] {
		if now.Sub(at) < window {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now)
	t.hits[key] = kept
	return len(kept) <= max
}

// forget clears a key's history (called on success, so one typo burst
// does not poison the next login five minutes later).
func (t *attemptTracker) forget(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.hits, key)
}

// signinIP extracts the throttle key from the request: the TCP peer's IP,
// never a proxy header (see handleSignin for why X-Forwarded-For would be
// a bypass). Malformed peers key as themselves — distinct garbage does not
// collide with real clients, and it still throttles garbage floods.
func signinIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
