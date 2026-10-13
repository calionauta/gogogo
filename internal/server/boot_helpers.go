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
	reviewchi "github.com/calionauta/gogogo/features/review/chi"
	chifeatures "github.com/calionauta/gogogo/features/todo/chi"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/demo"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/rbac"
)

// seedDemoAccounts signs the advertised demo pair up (idempotent: every
// boot after the first is a no-op). Split out of BootChi so the composition
// stays under the funlen gate; behavior unchanged.
func seedDemoAccounts(a *auth.Auth) {
	demoCtx, cancelDemo := context.WithCancel(context.Background())
	demo.Ensure(demoCtx, a)
	cancelDemo()
}

// applyRBAC builds one enforcer, promotes ADMIN_EMAILS rows, and wraps the
// mux in the middleware (pass-through when disabled, enforced denies when
// on — the flag never rewires anything).
func applyRBAC(a *auth.Auth, cfg *config.Config, queries *dbgen.Queries, mux *http.ServeMux) (http.Handler, error) {
	enforcer, err := rbac.NewEnforcer()
	if err != nil {
		return nil, fmt.Errorf("server: chi rbac: %w", err)
	}
	if n, err := promoteAdmins(cfg.AdminEmails, queries); err != nil {
		return nil, err
	} else if n > 0 {
		slog.Info("server: rbac admin promotion", "rows", n)
	}
	return rbac.Middleware(a, enforcer, cfg.RBACEnabled)(mux), nil
}

// wireTodoLLM builds the LLM clients and attaches the todo slice's queue,
// suggest and retry handlers. Split out of BootChi for the funlen gate.
// The clients are both consumed here (todo wiring, always compiled) and
// returned for the chat/genui constructions in BootChi (dropped by the
// genui trim): either side alone keeps the composition green.
func wireTodoLLM(todoH *chifeatures.Handler, chiQ *queue.Queue, cfg *config.Config) (real, simulated *llm.Client) {
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
func promoteAdmins(emails []string, queries *dbgen.Queries) (int, error) {
	n := 0
	for _, email := range emails {
		if _, err := queries.GetUserByEmail(context.Background(), email); err != nil {
			// Unowned address: skip silently (anyone can claim one, so
			// absence must not fail the boot). Real lookup failures do.
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return n, fmt.Errorf("server: promote lookup %s: %w", email, err)
		}
		if err := queries.SetUserRole(context.Background(), dbgen.SetUserRoleParams{
			Role: "admin", Updated: dbschema.Now(), Email: email,
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
) *reviewchi.Handler {
	g := reviewchi.NewGenkit()
	runner := &reviewchi.Runner{
		G:            g,
		Model:        reviewchi.DefineReviewModel(g, realLLM),
		Judge:        reviewchi.NewJudgeClient(cfg),
		Threshold:    cfg.Review.ConfidenceThreshold,
		JudgeTimeout: cfg.Review.Timeout,
		JudgeName:    cfg.Review.Model,
	}
	h := reviewchi.NewHandler(cfg, a, runner, realLLM)
	h.RegisterFlows()
	return h
}
