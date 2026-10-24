// SCOPE:core — Per-session request limiter red-proof.
package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
)

func TestRateLimitPerSession(t *testing.T) {
	h := rateLimit(2, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	hits := func(token string) int {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo", nil)
		// #nosec G124 — test-only request cookie; never traverses a network.
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := hits("session-a"); got != http.StatusOK {
		t.Fatalf("first request = %d", got)
	}
	if got := hits("session-a"); got != http.StatusOK {
		t.Fatalf("second request = %d", got)
	}
	if got := hits("session-a"); got != http.StatusTooManyRequests {
		t.Fatalf("third request = %d, want 429", got)
	}
	if got := hits("session-b"); got != http.StatusOK {
		t.Fatalf("another session = %d, want 200 (budget is per session)", got)
	}
}

func TestRateLimitIgnoresAnonymous(t *testing.T) {
	h := rateLimit(1, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for range 3 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("anonymous request = %d, want 200", rec.Code)
		}
	}
}

func TestRateLimiterCapsKeyMap(t *testing.T) {
	l := newRateLimiter(1)
	for i := range rateLimitMaxKeys {
		if !l.allow(fmt.Sprintf("key-%d", i)) {
			t.Fatalf("key %d refused before the cap", i)
		}
	}
	if l.allow("one-too-many") {
		t.Fatal("new key accepted past the hard cap")
	}
}
