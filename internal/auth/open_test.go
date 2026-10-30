package auth

import (
	"context"
	"net/http/httptest"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// TestOpenModeSession pins AUTH_MODE=open: with the synthetic session every
// request is the fixed local owner, without any cookie, and disabling it
// restores the fail-closed behaviour.
func TestOpenModeSession(t *testing.T) {
	sqlDB := testDB(t)
	a := testAuth(t, sqlDB)
	q := dbgen.New(sqlDB)

	req := httptest.NewRequestWithContext(t.Context(), "GET", "/", nil)
	if _, err := a.Session(req); err == nil {
		t.Fatal("login mode returned a session without a cookie")
	}

	u, err := EnsureOpenUser(context.Background(), q)
	if err != nil {
		t.Fatalf("ensure open user: %v", err)
	}
	if u.ID != OpenUserID {
		t.Fatalf("open user id = %q, want %q", u.ID, OpenUserID)
	}
	a.EnableOpen(u)

	sess, err := a.Session(req)
	if err != nil {
		t.Fatalf("open mode refused the request: %v", err)
	}
	if sess.User.ID != OpenUserID || sess.Role != "admin" {
		t.Fatalf("open session = %+v, want admin %s", sess, OpenUserID)
	}

	// Idempotent: a second boot reuses the same row.
	again, err := EnsureOpenUser(context.Background(), q)
	if err != nil || again.ID != OpenUserID {
		t.Fatalf("EnsureOpenUser not idempotent: %+v, %v", again, err)
	}

	a.DisableOpen()
	if _, err := a.Session(req); err == nil {
		t.Fatal("DisableOpen left a synthetic session behind")
	}
}
