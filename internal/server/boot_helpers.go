// Flat-stack boot helpers (demo seed, TOTP key wiring).
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/authviews"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/collab"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/rbac"
)

// seedDemoAccounts signs the advertised demo pair up and stamps their
// roles (demo1 admin, demo2 viewer) — idempotent, so every boot after the
// first is a role refresh. Split out of BootChi so the composition stays
// under the funlen gate. Runs BEFORE applyRBAC, so an ADMIN_EMAILS /
// VIEWER_EMAILS promotion still wins over the seeded default.
func seedDemoAccounts(a *auth.Auth, q *dbgen.Queries) {
	demoCtx, cancelDemo := context.WithCancel(context.Background())
	demo.Ensure(demoCtx, a, q)
	cancelDemo()
}

// applyRBAC builds one enforcer, promotes VIEWER_EMAILS then ADMIN_EMAILS
// rows, and wraps the mux in the middleware (pass-through when disabled,
// enforced denies when on — the flag never rewires anything). Viewers
// promote first so an address in both lists ends admin, never viewer.
// ctx is the boot context: promotion stops if the boot is cancelled.
// bootstrapSignup opens signup while the database has no account. A fresh
// install with production defaults (signup off) has no operator to create
// user #1; that is true for the desktop and for a licensed web deploy. The
// flag closes on the next boot, once a row exists.
func bootstrapSignup(cfg *config.Config, q *dbgen.Queries) {
	if cfg.SignupEnabled || q == nil {
		return
	}
	n, err := q.CountUsers(context.Background())
	if err != nil {
		slog.Warn("server: first-run check skipped, count users", "error", err)
		return
	}
	if n > 0 {
		return
	}
	cfg.SignupEnabled = true
	slog.Info("server: first-run signup enabled", "reason", "no local accounts yet")
}

// wireAuth applies the auth deployment switches and installs the standalone
// email-link pages. The renderer is a callback so internal/auth stays free of
// the feature/UI dependency.
func wireAuth(a *auth.Auth, cfg *config.Config) error {
	if err := a.SetAppURL(cfg.Auth.AppURL); err != nil {
		return err
	}
	a.SetSignupEnabled(cfg.SignupEnabled)
	a.SetMagicLinkBindBrowser(cfg.Auth.MagicLinkBindBrowser)
	wireAuthPages(a)
	wireAuthMail(a, cfg)
	// At-rest TOTP encryption is opt-in by env (AUTH_ENC_KEY): a malformed
	// key fails the boot rather than running half-encrypted.
	return wireTOTPKey(a, cfg)
}

// wireAuthPages installs the standalone email-link pages.
func wireAuthPages(a *auth.Auth) {
	a.SetMagicConfirmPage(func(w http.ResponseWriter, r *http.Request, email, token, bind, errMsg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		//nolint:contextcheck // templ components receive ctx from Render.
		_ = authviews.MagicConfirmPage(email, token, bind, errMsg).Render(r.Context(), w)
	})
	a.SetPasswordResetPage(func(w http.ResponseWriter, r *http.Request, email, token, errMsg string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		//nolint:contextcheck // templ components receive ctx from Render.
		_ = authviews.PasswordResetPage(email, token, errMsg).Render(r.Context(), w)
	})
}

// wireAuthMail installs SMTP delivery for verification/reset and adapts it to
// the link-shaped magic sender. Empty SMTP config leaves both closed.
func wireAuthMail(a *auth.Auth, cfg *config.Config) {
	a.SetRequireEmailVerification(cfg.Auth.RequireEmailVerification)
	a.SetSessionIdle(cfg.Auth.SessionIdleTimeout)
	if cfg.Auth.SMTP.Addr == "" || cfg.Auth.SMTP.From == "" {
		return
	}
	mailer := auth.NewSMTPMailer(cfg.Auth.SMTP.Addr, cfg.Auth.SMTP.From, cfg.Auth.SMTP.Username, cfg.Auth.SMTP.Password)
	a.SetMailer(mailer)
	a.SetMagicSender(func(to, link string) error {
		return mailer(to, "Sign in to "+cfg.AppName, "Open this link to sign in:\n\n"+link+"\n")
	})
}

// Audit retention. Thirty days is the template's default; an operator with a
// SIEM exports through /config/audit.csv before the cutoff. The boot pass
// handles a restart; the ticker handles an instance that stays up for months.
const (
	auditRetention     = 30 * 24 * time.Hour
	auditPruneInterval = 24 * time.Hour
	auditPruneTimeout  = 30 * time.Second
	auditPruneBatch    = 1000

	// opReceiptRetention bounds the dedupe window for replayed mutations. It
	// must OUTLIVE the client's outbox entry: the browser drops a queued op
	// after a week, so a receipt that expires sooner would let a late replay
	// apply a second time — the exact bug the receipt exists to prevent.
	opReceiptRetention = 14 * 24 * time.Hour
)

// pruneSecurityEvents deletes rows older than auditRetention in bounded
// batches. A single large DELETE would hold SQLite's one write lock for the
// whole statement and block logins; 1000 rows per statement keeps the worst
// case short and releases the lock between batches. Bounded by a timeout so a
// locked database cannot hold the boot or the ticker forever; the next run
// retries. Best-effort by design: retention is housekeeping, not an auth
// decision.
func pruneSecurityEvents(ctx context.Context, q *dbgen.Queries) {
	ctx, cancel := context.WithTimeout(ctx, auditPruneTimeout)
	defer cancel()
	cutoff := time.Now().UTC().Add(-auditRetention).Format(dbschema.TimeFormat)
	for {
		deleted, err := q.DeleteSecurityEventsBatchBefore(ctx, dbgen.DeleteSecurityEventsBatchBeforeParams{
			Created: cutoff, Limit: auditPruneBatch,
		})
		if err != nil {
			observability.Warn(ctx, "server: security-event prune failed",
				append(observability.WithComponent("audit", "core"), "error", err)...)
			return
		}
		if deleted < auditPruneBatch {
			return
		}
	}
}

// startRetention runs the housekeeping prunes once now and then daily until
// bootCtx is cancelled at shutdown. The goroutine has one select, so it cannot
// outlive the stack. Both prunes are best-effort: retention is housekeeping,
// never a decision the request path depends on.
func startRetention(ctx context.Context, q *dbgen.Queries) {
	pruneSecurityEvents(ctx, q)
	pruneOpReceipts(ctx, q)
	go func() {
		ticker := time.NewTicker(auditPruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pruneSecurityEvents(ctx, q)
				pruneOpReceipts(ctx, q)
			}
		}
	}()
}

// pruneOpReceipts deletes dedupe receipts past their window, in the same
// bounded batches as the audit prune (one large DELETE would hold SQLite's
// single write lock and stall mutations).
func pruneOpReceipts(ctx context.Context, q *dbgen.Queries) {
	ctx, cancel := context.WithTimeout(ctx, auditPruneTimeout)
	defer cancel()
	cutoff := time.Now().UTC().Add(-opReceiptRetention).Format(dbschema.TimeFormat)
	for {
		deleted, err := q.DeleteOpReceiptsBatchBefore(ctx, dbgen.DeleteOpReceiptsBatchBeforeParams{
			Created: cutoff, Limit: auditPruneBatch,
		})
		if err != nil {
			observability.Warn(ctx, "server: op-receipt prune failed",
				append(observability.WithComponent("offline-sync", "plugin"), "error", err)...)
			return
		}
		if deleted < auditPruneBatch {
			return
		}
	}
}

// demoQueries builds the shared query handle and seeds the demo pair only
// when the deploy opted in. One call keeps BootChi inside the funlen gate
// and the production default free of published credentials.
func demoQueries(cfg *config.Config, a *auth.Auth, sqlDB *sql.DB) *dbgen.Queries {
	q := dbgen.New(sqlDB)
	if cfg.DemoAccounts {
		seedDemoAccounts(a, q)
		// Demo accounts are always operator-capable (both dev and prod demo):
		// demo1 is published demo credentials, so showing DagNats console
		// is the point of the demo. Production builds that disable demo
		// accounts stay closed.
		if !slices.Contains(cfg.AdminEmails, demo.Email) {
			cfg.AdminEmails = append(cfg.AdminEmails, demo.Email)
		}
	} else {
		demo.Disable(context.Background(), q)
	}
	return q
}

func applyRBAC(
	ctx context.Context, a *auth.Auth, cfg *config.Config, queries *dbgen.Queries, mux http.Handler,
) (http.Handler, error) {
	enforcer, err := rbac.NewEnforcer()
	if err != nil {
		return nil, fmt.Errorf("server: chi rbac: %w", err)
	}
	// Optional dynamic policy source: seed the casbin_rule table from the
	// embedded policy (once) and load the enforcer from it, so the rules
	// are editable at runtime. Default (embedded) reads no DB.
	if cfg.RBACPolicySource == "db" {
		if serr := rbac.SeedEmbeddedPolicy(ctx, queries); serr != nil {
			return nil, fmt.Errorf("server: chi rbac seed: %w", serr)
		}
		if enforcer, err = rbac.NewEnforcerWithAdapter(rbac.NewDBAdapter(queries)); err != nil {
			return nil, fmt.Errorf("server: chi rbac (db): %w", err)
		}
		slog.Info("server: rbac policy source", "source", "db")
	}
	if n, err := promoteRole(ctx, cfg.ViewerEmails, rbac.RoleViewer, queries); err != nil {
		return nil, err
	} else if n > 0 {
		slog.Info("server: rbac viewer promotion", "rows", n)
	}
	if n, err := promoteAdmins(ctx, cfg.AdminEmails, queries); err != nil {
		return nil, err
	} else if n > 0 {
		slog.Info("server: rbac admin promotion", "rows", n)
	}
	return rbac.MiddlewareWithPermissive(a, enforcer, cfg.RBACEnabled, cfg.RBACPermissive)(mux), nil
}

// promoteAdmins sets users.role='admin' for existing rows matching the
// operator list (ADMIN_EMAILS). Promotion only, never provisioning: unknown
// addresses are skipped silently (anyone can claim an unowned address, so
// absence must not fail the boot). Returns rows actually promoted.
func promoteAdmins(ctx context.Context, emails []string, queries *dbgen.Queries) (int, error) {
	return promoteRole(ctx, emails, "admin", queries)
}

// promoteRole sets users.role for existing rows matching the operator
// list. Shared by admin and viewer promotion: same promotion-only,
// skip-silently contract, role parameterized. Returns rows promoted.
func promoteRole(ctx context.Context, emails []string, role string, queries *dbgen.Queries) (int, error) {
	n := 0
	for _, email := range emails {
		if _, err := queries.GetUserByEmail(ctx, email); err != nil {
			// Unowned address: skip silently (anyone can claim one, so
			// absence must not fail the boot). Real lookup failures do.
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return n, fmt.Errorf("server: promote lookup %s: %w", email, err)
		}
		if err := queries.SetUserRole(ctx, dbgen.SetUserRoleParams{
			Role: role, Updated: dbschema.Now(), Email: email,
		}); err != nil {
			return n, fmt.Errorf("server: promote %s: %w", email, err)
		}
		n++
	}
	return n, nil
}

// wireTOTPKey installs the seal key: AUTH_ENC_KEY when set (malformed
// fails the boot — an explicit operator error must not silently become a
// different key), else the auto-created file in the data dir. A file
// failure warns and continues keyless: boot availability wins, and
// enrollment fails closed per operation without a key.
func wireTOTPKey(a *auth.Auth, cfg *config.Config) error {
	key := cfg.Auth.TOTPEncKey
	if raw := os.Getenv("AUTH_ENC_KEY"); raw != "" && len(key) == 0 {
		return errors.New("server: chi auth: malformed AUTH_ENC_KEY (want 64 hex chars)")
	}
	if len(key) == 0 {
		fk, ferr := auth.LoadOrCreateKeyFile(cfg.DataDir)
		if ferr != nil {
			slog.Warn("server: totp key file unavailable, TOTP enrollment disabled", "error", ferr)
			return nil
		}
		key = fk[:]
	}
	if err := a.SetTOTPEncKey(key); err != nil {
		return fmt.Errorf("server: chi auth: %w", err)
	}
	return nil
}

// bootFail tears down the two resources every mid-boot failure must release
// (the consumer goroutine and the pool) and wraps the cause. Every early
// return in BootChi goes through it: the pair was repeated seven times, which
// is what pushed BootChi past the statement gate, and one of the copies had
// already drifted (a missing cancelConsumer).
func bootFail(cancelConsumer context.CancelFunc, sqlDB *sql.DB, msg string, err error) (*ChiStack, error) {
	cancelConsumer()
	_ = sqlDB.Close()
	return nil, fmt.Errorf("server: chi %s: %w", msg, err)
}

// bootCollab builds the shared Loro doc store and its snapshot persister.
// Extracted from BootChi for the funlen gate: the pair travels together, so
// one helper keeps the two lines that must agree in one place.
func bootCollab(cfg *config.Config, queries *dbgen.Queries) (collab.Persister, *collab.DocStore, error) {
	persister, err := collab.SnapshotStore(cfg.Collab.Snapshots, queries)
	if err != nil {
		return nil, nil, err
	}
	docs := collab.NewDocStore()
	docs.SetPersister(persister)
	return persister, docs, nil
}
