// SCOPE:core — Demo-user seed for fresh installs (the legacy SeedDefaults pair)
//
// The old stack seeded a demo account on first boot (db/seed.go) so a fresh
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

// The pairs the login form advertises. Same values the legacy seed used
// (db/seed.go seeded both), so demos and docs keep working: demo1 is
// prefilled, demo2 is the second browser for contention.
const (
	Email     = "demo1@demo.app"
	Email2    = "demo2@demo.app"
	Password  = "demo1234456"
	demoName  = "Demo User"
	demoName2 = "Demo User 2"
)

// Ensure seeds the demo accounts. Best-effort by design: a failure here
// logs and continues (a fresh install without the demo users is a UX
// regression, not a boot failure), and a duplicate is the normal repeat
// case.
func Ensure(ctx context.Context, a *auth.Auth) {
	seedOne(ctx, a, Email, Password, demoName)
	seedOne(ctx, a, Email2, Password, demoName2)
}

func seedOne(ctx context.Context, a *auth.Auth, email, password, name string) {
	if _, _, err := a.Signup(ctx, email, password, name); err != nil {
		if !errors.Is(err, auth.ErrBadCredentials) {
			log.Printf("demo: seed skipped: %v", err)
		}
	}
}
