// SCOPE:core — Demo-user seed for fresh installs (the PB-era SeedDefaults pair)
//
// PocketBase seeded a demo account on first boot (db/seed.go) so a fresh
// clone had a way in. The flat stack keeps that contract: one idempotent
// call at boot (internal/server) and on the native window's first run
// (cmd/gui) signs the same pair up. Signup is idempotent by design — a
// duplicate email answers ErrBadCredentials, which is exactly the
// "already seeded" case.
package demo

import (
	"context"
	"errors"
	"log"

	"github.com/calionauta/gogogo/internal/auth"
)

// The pair the login form advertises as its placeholder. Same values the
// PB seed used, so demos and docs keep working.
const (
	Email    = "demo1@demo.app"
	Password = "demo1234456"
)

// Ensure seeds the demo account. Best-effort by design: a failure here
// logs and continues (a fresh install without the demo user is a UX
// regression, not a boot failure), and a duplicate is the normal repeat
// case.
func Ensure(ctx context.Context, a *auth.Auth) {
	if _, _, err := a.Signup(ctx, Email, Password, "Demo User"); err != nil {
		if !errors.Is(err, auth.ErrBadCredentials) {
			log.Printf("demo: seed skipped: %v", err)
		}
	}
}
