// SCOPE:core — AUTH_MODE=open: one local owner, no login.
package auth

import (
	"context"
	"errors"
	"time"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// OpenUserID is the fixed identity of the no-login mode. It is a normal
// (if synthetic) user row, so every owner-scoped query keeps working.
const OpenUserID = "00000000-0000-7000-8000-000000000001"

// EnableOpen installs the no-login session for every request. The role is
// admin because a single local owner owns the app; operator surfaces stay
// closed anyway (they also require an ADMIN_EMAILS match).
func (a *Auth) EnableOpen(u *User) {
	a.openMu.Lock()
	defer a.openMu.Unlock()
	a.open = &ValidatedSession{
		User:      u,
		Role:      "admin",
		SessionID: "open",
		Expires:   time.Now().UTC().Add(100 * 365 * 24 * time.Hour),
	}
}

// DisableOpen removes the synthetic session (tests and mode changes).
func (a *Auth) DisableOpen() {
	a.openMu.Lock()
	defer a.openMu.Unlock()
	a.open = nil
}

// OpenMode reports whether AUTH_MODE=open installed the synthetic session.
// UI code uses it to hide login-only affordances; never for an access
// decision (the session itself is the decision).
func (a *Auth) OpenMode() bool {
	return a.openSessionState() != nil
}

func (a *Auth) openSessionState() *ValidatedSession {
	a.openMu.RLock()
	defer a.openMu.RUnlock()
	return a.open
}

// EnsureOpenUser creates the fixed local owner once. Password hash "!" is the
// locked sentinel already used by the demo/seed paths: no password login.
func EnsureOpenUser(ctx context.Context, q *dbgen.Queries) (*User, error) {
	if q == nil {
		return nil, errors.New("auth: open user needs queries")
	}
	row, err := q.GetUser(ctx, OpenUserID)
	if err == nil {
		return &User{ID: row.ID, Email: row.Email}, nil
	}
	now := time.Now().UTC().Format(dbschema.TimeFormat)
	if _, cerr := q.CreateUser(ctx, dbgen.CreateUserParams{
		ID: OpenUserID, Email: "local@device", PasswordHash: "!",
		Name: "Local owner", Created: now, Updated: now,
	}); cerr != nil {
		return nil, cerr
	}
	return &User{ID: OpenUserID, Email: "local@device"}, nil
}
