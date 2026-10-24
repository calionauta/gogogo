// SCOPE:core — session user-agent capture and sanitisation.
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignupCapturesUserAgent(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	mux := securityMux(a)
	body := `{"email":"agent@example.com","password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/auth/signup/credential",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) Chrome/120.0")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("signup = %d (%s)", rec.Code, rec.Body.String())
	}
	user, err := a.q.GetUserByEmail(ctx, "agent@example.com")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	rows, err := a.q.ListSessionsByUser(ctx, user.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("sessions = %d (%v)", len(rows), err)
	}
	if rows[0].UserAgent != "Mozilla/5.0 (X11; Linux x86_64) Chrome/120.0" {
		t.Fatalf("user agent = %q", rows[0].UserAgent)
	}
}

func TestCleanUserAgentCapsAndStripsControls(t *testing.T) {
	got := cleanUserAgent("  a\x00b\n" + strings.Repeat("x", maxUserAgentLen+10))
	if strings.ContainsAny(got, "\x00\n") {
		t.Fatalf("control characters survived: %q", got)
	}
	if len(got) > maxUserAgentLen {
		t.Fatalf("user agent length = %d, want <= %d", len(got), maxUserAgentLen)
	}
}
