// SCOPE:core — SessionByToken tests (the transport-free session path)
//
// Session validates cookies over *http.Request; SessionByToken resolves
// a raw token without a request — what cmd/gui's persisted "remember
// me" file needs. The PB-era features/auth suite pinned the same
// round-trip through Login + ResolveOwner; these tests pin the flat
// equivalents: token → user, unknown token fails, and a revoked session
// (logout) fails too.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

func newSessionAuth(t *testing.T) *Auth {
	t.Helper()
	sqlDB, err := db.OpenAt(t.TempDir() + "/session-token.db")
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if merr := db.Migrate(sqlDB); merr != nil {
		t.Fatalf("db migrate: %v", merr)
	}
	a, err := New(dbgen.New(sqlDB), false)
	if err != nil {
		t.Fatalf("auth new: %v", err)
	}
	return a
}

func TestSessionByTokenRoundTrip(t *testing.T) {
	a := newSessionAuth(t)
	ctx := context.Background()
	_, token, err := a.Signup(ctx, "token@example.com", "Correct-horse-99!", "Token")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	sess, err := a.SessionByToken(ctx, token)
	if err != nil {
		t.Fatalf("SessionByToken: %v", err)
	}
	if sess.User.Email != "token@example.com" || sess.User.ID == "" {
		t.Fatalf("resolved user = %+v, want the signed-up identity", sess.User)
	}
	if sess.Expires.IsZero() {
		t.Error("resolved session has no expiry")
	}
}

func TestSessionByTokenRejectsUnknownToken(t *testing.T) {
	a := newSessionAuth(t)
	if _, err := a.SessionByToken(context.Background(), "not-a-real-token"); err == nil {
		t.Fatal("SessionByToken accepted an unknown token")
	}
	if _, err := a.SessionByToken(context.Background(), ""); err == nil {
		t.Fatal("SessionByToken accepted an empty token")
	}
}

func TestSessionByTokenRejectsRevokedSession(t *testing.T) {
	a := newSessionAuth(t)
	ctx := context.Background()
	_, token, err := a.Signup(ctx, "revoked@example.com", "Correct-horse-99!", "Revoked")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	a.Logout(ctx, token)
	if _, err := a.SessionByToken(ctx, token); err == nil {
		t.Fatal("SessionByToken resolved a logged-out session (revocation not honored)")
	}
}

// TestSessionProbeEndpoint pins the ported /api/session contract the
// shared banner polls: anonymous requests report authed=false (200, not
// 401 — a probe is a poll, not a gate), a live session reports true, and
// a logged-out session reports false again. Ported from the PB-era
// features/auth/wiring.go endpoint, which died with the framework boot
// while its client (static/session.js) kept polling.
func TestSessionProbeEndpoint(t *testing.T) {
	a := newSessionAuth(t)
	ctx := context.Background()
	_, token, err := a.Signup(ctx, "probe@example.com", "Correct-horse-99!", "Probe")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()

	probe := func(token string) bool {
		t.Helper()
		req, rErr := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/session", nil)
		if rErr != nil {
			t.Fatalf("probe req: %v", rErr)
		}
		if token != "" {
			req.AddCookie(a.SetSessionCookie(token))
		}
		resp, dErr := http.DefaultClient.Do(req)
		if dErr != nil {
			t.Fatalf("probe: %v", dErr)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("probe status = %d, want 200 (never 401)", resp.StatusCode)
		}
		var out struct {
			Authed bool `json:"authed"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode probe: %v", err)
		}
		return out.Authed
	}

	if probe("") {
		t.Error("anonymous probe reports authed=true")
	}
	if !probe(token) {
		t.Error("live session probe reports authed=false")
	}
	a.Logout(ctx, token)
	if probe(token) {
		t.Error("revoked session still probes authed=true")
	}
}
