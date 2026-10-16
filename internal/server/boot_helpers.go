// SCOPE:core — Flat-stack boot helpers (demo seed, TOTP key wiring).
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/calionauta/gogogo/config"
	reviewhandlers "github.com/calionauta/gogogo/features/review/handlers"
	todohandlers "github.com/calionauta/gogogo/features/todo/handlers"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
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
func applyRBAC(
	ctx context.Context, a *auth.Auth, cfg *config.Config, queries *dbgen.Queries, mux *http.ServeMux,
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

// wireTodoDisplay copies the display-time config into the todo handler:
// version badge, offline label and realtime label. Extracted from BootChi
// to keep boot under the function-length gate; it is plain assignment with
// no control flow of its own.
func wireTodoDisplay(todoH *todohandlers.Handler, cfg *config.Config) {
	todoH.BuildLabel = cfg.BuildLabel
	todoH.BuildCommit = cfg.BuildCommit
	todoH.OfflineSyncEnabled = cfg.OfflineSync.Enabled
	todoH.NATSEnabled = cfg.NATS.Enabled
}

// wireTodoLLM builds the LLM clients and attaches the todo slice's queue,
// suggest and retry handlers. Split out of BootChi for the funlen gate.
// The clients are both consumed here (todo wiring, always compiled) and
// returned for the chat/genui constructions in BootChi (dropped by the
// genui trim): either side alone keeps the composition green.
func wireTodoLLM(todoH *todohandlers.Handler, chiQ *queue.Queue, cfg *config.Config) (real, simulated *llm.Client) {
	real = llm.New(cfg.GoAI.APIKey)
	if v := os.Getenv("SIMULATE_LLM"); v != "false" {
		simulated = llm.NewSimulated()
	}
	todoH.SetQueue(chiQ)
	todoH.SetLLMClient(real)
	if simulated != nil {
		todoH.SetSimulatedLLMClient(simulated)
	}
	todoH.RegisterSuggestHandlers(chiQ.Registry())
	todoH.RegisterRetryHandlers(chiQ.Registry())
	return real, simulated
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

// bootReview wires the Genkit review-loop slice: one shared Genkit
// instance, the gogogo/llm model action over the real LLM client (metered
// exactly like todo suggest when a Biller is set), and the jev-compatible
// judge from JEV_* env. Lives here (not chi.go) for the file-size gate;
// the review trim strips this whole block (see the installer unit).
func bootReview(
	cfg *config.Config, a *auth.Auth, realLLM *llm.Client,
) *reviewhandlers.Handler {
	g := reviewhandlers.NewGenkit()
	runner := &reviewhandlers.Runner{
		G:            g,
		Model:        reviewhandlers.DefineReviewModel(g, realLLM),
		Judge:        reviewhandlers.NewJudgeClient(cfg),
		Threshold:    cfg.Review.ConfidenceThreshold,
		JudgeTimeout: cfg.Review.Timeout,
	}
	h := reviewhandlers.NewHandler(cfg, a, runner, realLLM)
	h.RegisterFlows()
	return h
}

// startCrudConsumer runs the JetStream CRUD consumer for cross-instance
// convergence until the boot context is cancelled. It is nil-safe on js
// (RunCrudConsumer no-ops when NATS is disabled). Split from chi.go for
// the file-sizes gate; the CRUD indexer it threads through must match the
// one the HTTP path uses or the two write paths diverge.
func startCrudConsumer(
	ctx context.Context, js nats.JetStreamLike, appName string,
	todoH *todohandlers.Handler, hub *queue.SSEHub,
) {
	go func() {
		if cerr := todohandlers.RunCrudConsumer(ctx, js, appName, todoH.Q, todoH.Index, todoH.DB, hub); cerr != nil {
			slog.Error("server: chi crud consumer stopped", "error", cerr)
		}
	}()
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
