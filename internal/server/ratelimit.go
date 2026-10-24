// SCOPE:core — In-process per-session request limiter.
//
// The login surface has its own tighter budget (internal/auth/ratelimit.go).
// This one bounds the authenticated dynamic surface so a stock single-box
// deployment is not an unbounded request sink; a fleet should still throttle
// at the edge, which is the documented primary. The key is the session
// cookie, never the client IP: behind the tunnel every request arrives from
// the same proxy address, so an IP bucket would throttle the whole site.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/calionauta/gogogo/internal/auth"
)

// rateBucket is a fixed one-minute window. A sliding log would be more
// precise and cost a slice per session; a minute is the granularity this
// limit is stated in.
type rateBucket struct {
	start time.Time
	count int
}

// rateLimiter is safe for concurrent use and bounded by lazy sweeping.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
	rpm     int
	now     func() time.Time
}

// rateLimitSweepAt bounds the map's growth; a sweep runs only past it.
// rateLimitMaxKeys is the hard cap: random cookie values must not turn the
// limiter itself into an unbounded memory sink.
const (
	rateLimitSweepAt = 4096
	rateLimitMaxKeys = 4096
)

func newRateLimiter(rpm int) *rateLimiter {
	return &rateLimiter{buckets: map[string]rateBucket{}, rpm: rpm, now: time.Now}
}

// allow counts one request and reports whether it fits the budget.
func (l *rateLimiter) allow(key string) bool {
	if l.rpm <= 0 {
		return true
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.buckets[key]; !exists && len(l.buckets) >= rateLimitMaxKeys {
		l.sweep(now)
		if len(l.buckets) >= rateLimitMaxKeys {
			return false
		}
	}
	b := l.buckets[key]
	if b.start.IsZero() || now.Sub(b.start) >= time.Minute {
		b = rateBucket{start: now}
	}
	b.count++
	l.buckets[key] = b
	if len(l.buckets) > rateLimitSweepAt {
		l.sweep(now)
	}
	return b.count <= l.rpm
}

// sweep drops windows older than two minutes. Called only when the map grows,
// so the common path stays one map lookup and one assignment.
func (l *rateLimiter) sweep(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.start) >= 2*time.Minute {
			delete(l.buckets, k)
		}
	}
}

// rateLimitKey hashes the session token: the map must not keep raw cookies
// around, and the token itself is the identity the limit is about.
func rateLimitKey(r *http.Request) string {
	c, err := r.Cookie(auth.CookieName)
	if err != nil || c.Value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(c.Value))
	return hex.EncodeToString(sum[:16])
}

// rateLimit wraps an authenticated request in the per-session budget.
// Signed-out requests pass: their expensive surfaces (signin, signup,
// magic request) already carry a dedicated throttle, and an IP bucket
// behind a proxy would be one global bucket.
func rateLimit(rpm int, next http.Handler) http.Handler {
	limiter := newRateLimiter(rpm)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rpm <= 0 {
			next.ServeHTTP(w, r)
			return
		}
		key := rateLimitKey(r)
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !limiter.allow(key) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
