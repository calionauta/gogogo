// SCOPE:core — Auth signin throttle tests (per-IP password budget).

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSigninThrottlePerIP pins the password-guessing throttle at the HTTP
// layer: past the budget the endpoint keeps answering the same 401 (no
// oracle), a verified password still clears the key, and a second client
// IP is unaffected. Burn the throttle and a good login from the same IP
// fails identically — that indistinguishability IS the contract.
func TestSigninThrottlePerIP(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	if _, _, err := a.Signup(ctx, "throttle@example.com", "Correct-horse-99!", ""); err != nil {
		t.Fatalf("signup: %v", err)
	}
	mux := a.Routes()
	post := func(ip, password string) int {
		body := `{"credential":"throttle@example.com","password":"` + password + `"}`
		req := httptest.NewRequestWithContext(ctx, http.MethodPost,
			"/api/auth/signin/credential", strings.NewReader(body))
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code
	}
	// Sanity: wrong password is 401, right password is 200, same IP.
	if code := post("10.0.0.1", "wrong"); code != http.StatusUnauthorized {
		t.Fatalf("bad password = %d, want 401", code)
	}
	if code := post("10.0.0.1", "Correct-horse-99!"); code != http.StatusOK {
		t.Fatalf("good password = %d, want 200", code)
	}
	// Burn the budget from a second IP (success above cleared the first).
	for i := range signinAttemptMax {
		if code := post("10.0.0.2", "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("burn %d = %d, want steady 401", i, code)
		}
	}
	// Over budget: even the RIGHT password answers 401, identically.
	if code := post("10.0.0.2", "Correct-horse-99!"); code != http.StatusUnauthorized {
		t.Fatalf("throttled good password = %d, want indistinguishable 401", code)
	}
	// A third IP is unaffected.
	if code := post("10.0.0.3", "Correct-horse-99!"); code != http.StatusOK {
		t.Fatalf("other IP good password = %d, want 200", code)
	}
}
