// SCOPE:core — Demo-user seed for fresh installs
//
// A fresh clone needs a way in: one idempotent call at boot
// (internal/server) and on the native window's first run (cmd/gui) signs
// the same pair up. Signup is idempotent by design — a duplicate email
// answers ErrBadCredentials, which is exactly the "already seeded" case.
//
// The pair also carries fixed roles so the login card's promise ("Demo 1
// is the operator, Demo 2 the restricted viewer") is true by construction
// instead of depending on ADMIN_EMAILS/VIEWER_EMAILS being set. Role
// promotion from those env lists still runs AFTER the seed at boot, so an
// operator list always wins over the seeded default.
package demo

import (
	"context"
	"errors"
	"log/slog"

	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

// The pairs the login form advertises: demo1 is prefilled, demo2 is the
// second browser for contention.
const (
	Email     = "demo1@demo.app"
	Email2    = "demo2@demo.app"
	Password  = "demo1234456"
	demoName  = "Demo User"
	demoName2 = "Demo User 2"
)

// Seeded roles. The strings mirror internal/rbac (RoleAdmin / RoleViewer)
// but are kept literal so this core package does not pull the casbin
// dependency into lean native targets (cmd/gui builds CGO-free).
const (
	roleAdmin  = "admin"
	roleViewer = "viewer"
	roleUser   = "user"
)

type demoAccount struct {
	email, password, name, role string
}

// seededAccounts is the single source for who the demo pair is and what
// each may do. Order matters only for logs.
var seededAccounts = []demoAccount{
	{Email, Password, demoName, roleAdmin},
	{Email2, Password, demoName2, roleViewer},
}

// Disable closes the published demo credentials on a deployment that did
// not opt in. The rows stay (their data remains), but the password becomes
// unusable, the role drops to plain user, and sessions plus pending magic
// links are revoked. Idempotent: a boot after the first is a no-op.
func Disable(ctx context.Context, q *dbgen.Queries) {
	if q == nil {
		return
	}
	for _, acc := range seededAccounts {
		row, err := q.GetUserByEmail(ctx, acc.email)
		if err != nil {
			continue // never seeded
		}
		if row.PasswordHash == "!" && row.Role == roleUser {
			continue // already disabled
		}
		if perr := q.UpdateUserPassword(ctx, dbgen.UpdateUserPasswordParams{
			PasswordHash: "!", Updated: dbschema.Now(), ID: row.ID,
		}); perr != nil {
			slog.WarnContext(ctx, "demo: disable password", "email", acc.email, "error", perr)
			continue
		}
		if rerr := q.SetUserRole(ctx, dbgen.SetUserRoleParams{
			Role: roleUser, Updated: dbschema.Now(), Email: acc.email,
		}); rerr != nil {
			slog.WarnContext(ctx, "demo: disable role", "email", acc.email, "error", rerr)
		}
		_ = q.DeleteUserSessions(ctx, row.ID)
		_ = q.DeleteMagicTokensForUser(ctx, row.ID)
		slog.InfoContext(ctx, "demo: published credentials disabled", "email", acc.email)
	}
}

// Ensure seeds the demo accounts and their roles. Best-effort by design: a
// failure here logs and continues (a fresh install without the demo users
// is a UX regression, not a boot failure), and a duplicate is the normal
// repeat case. Roles are rewritten every boot, so a fresh clone and an
// upgraded one converge on the same demo surface.
func Ensure(ctx context.Context, a *auth.Auth, q *dbgen.Queries) {
	if a == nil || q == nil {
		slog.WarnContext(ctx, "demo: seed skipped (nil dependency)")
		return
	}
	for _, acc := range seededAccounts {
		seedOne(ctx, a, q, acc)
	}
}

// restoreDisabledPassword resets only the explicit "!" marker left by
// Disable. A user-changed password is left alone.
func restoreDisabledPassword(ctx context.Context, a *auth.Auth, q *dbgen.Queries, acc demoAccount) {
	row, err := q.GetUserByEmail(ctx, acc.email)
	if err != nil || row.PasswordHash != "!" {
		return
	}
	if rerr := a.SetPassword(ctx, row.ID, acc.password); rerr != nil {
		slog.WarnContext(ctx, "demo: restore password", "email", acc.email, "error", rerr)
		return
	}
	slog.InfoContext(ctx, "demo: published password restored", "email", acc.email)
}

func seedOne(ctx context.Context, a *auth.Auth, q *dbgen.Queries, acc demoAccount) {
	if _, _, err := a.Signup(ctx, acc.email, acc.password, acc.name); err != nil {
		if !errors.Is(err, auth.ErrBadCredentials) {
			slog.WarnContext(ctx, "demo: seed skipped", "error", err)
			return
		}
		// Already seeded (duplicate email): restore a pair disabled by a
		// previous production boot, then refresh the role.
		restoreDisabledPassword(ctx, a, q, acc)
	} else {
		slog.InfoContext(ctx, "demo: seed created", "email", acc.email)
	}
	// Role is idempotent every boot: a role column reset converges back to
	// the advertised demo surface on the next start.
	if err := q.SetUserRole(ctx, dbgen.SetUserRoleParams{
		Role: acc.role, Updated: dbschema.Now(), Email: acc.email,
	}); err != nil {
		slog.WarnContext(ctx, "demo: role skipped", "email", acc.email, "error", err)
	}
	// Demo accounts are operator-created fixtures, not public signups.
	if row, err := q.GetUserByEmail(ctx, acc.email); err == nil {
		_ = q.SetEmailVerified(ctx, dbgen.SetEmailVerifiedParams{
			EmailVerifiedAt: dbschema.Now(), Updated: dbschema.Now(), ID: row.ID,
		})
	}
}
