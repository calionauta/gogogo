// SCOPE:feature — Activation page for the distribution gate.
//
// LICENSE_MODE=license: the buyer pastes the token the owner minted offline.
// LICENSE_MODE=account: the same token, bound to a user id; activation adopts
// that identity locally and signs the user in, so the app and the server agree
// on the owner of every row that later syncs.
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/license/components"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/cloudcfg"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/license"
	"github.com/calionauta/gogogo/internal/observability"
)

// Handler serves /activate.
type Handler struct {
	cfg   *config.Config
	store *license.Store
	a     *auth.Auth
	q     *dbgen.Queries
}

// NewHandler builds the activation slice.
func NewHandler(cfg *config.Config, store *license.Store, a *auth.Auth, q *dbgen.Queries) *Handler {
	if cfg == nil || store == nil || a == nil || q == nil {
		panic("license: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, store: store, a: a, q: q}
}

// Routes returns the activation router.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/activate", h.page)
	r.Post("/activate", h.activate)
	return r
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	if h.cfg.LicenseMode == config.LicenseNone || h.store.Valid(time.Now()) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	h.render(w, r, "")
}

func (h *Handler) activate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.render(w, r, "Invalid form submission.")
		return
	}
	raw := strings.TrimSpace(r.PostFormValue("token"))
	if raw == "" {
		h.render(w, r, "Paste the token you received.")
		return
	}
	password := r.PostFormValue("password")
	if password != "" && len(password) < 8 {
		h.render(w, r, "The local password needs at least 8 characters.")
		return
	}
	claims, err := h.store.Activate(raw, time.Now())
	if err != nil {
		h.render(w, r, activationError(err))
		return
	}

	// An owner-minted token can carry the cloud endpoint, so activation also
	// connects the device without a second form (applies on next launch).
	if strings.TrimSpace(claims.SyncURL) != "" {
		syncCfg := cloudcfg.Config{URL: claims.SyncURL, Token: claims.SyncToken}
		if serr := cloudcfg.Save(cloudcfg.Path(h.cfg.DataDir), syncCfg); serr != nil {
			observability.Warn(r.Context(), "license: sync endpoint not saved",
				append(observability.WithComponent("license", "feature"), "error", serr)...)
		}
	}

	if h.cfg.LicenseMode == config.LicenseAccount {
		if aerr := h.activateAccount(w, r, claims, password); aerr != nil {
			return
		}
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ensureAccount adopts the token's server identity locally: same user id, same
// email. The password is the locked sentinel, so password login on this
// profile stays closed until a local password is set from /account.
func (h *Handler) ensureAccount(ctx context.Context, sub, email string) error {
	if _, err := h.q.GetUser(ctx, sub); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if strings.TrimSpace(email) == "" {
		email = sub + "@device"
	}
	now := time.Now().UTC().Format(dbschema.TimeFormat)
	_, err := h.q.CreateUser(ctx, dbgen.CreateUserParams{
		ID: sub, Email: email, PasswordHash: "!",
		Name: email, Created: now, Updated: now,
	})
	return err
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if rerr := components.ActivatePage(components.PageData{
		Mode:  h.cfg.LicenseMode,
		Error: errMsg,
	}).Render(r.Context(), w); rerr != nil {
		http.Error(w, "activation page unavailable", http.StatusInternalServerError)
	}
}

// activationError turns a verifier error into something a buyer can act on.
func activationError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "expired"):
		return "This token has expired. Ask the seller for a renewal."
	case strings.Contains(msg, "signature"):
		return "This token was not issued for this app (signature mismatch)."
	case strings.Contains(msg, "no public key"):
		return "This build has no license key configured; contact the seller."
	default:
		return "That token is not valid. Check for a missing line or copy it again."
	}
}

// activateAccount adopts the token's identity, optionally sets the local
// password, and signs the user in. A nil error means the response is complete.
func (h *Handler) activateAccount(
	w http.ResponseWriter, r *http.Request, claims *license.Claims, password string,
) error {
	sub := strings.TrimSpace(claims.Sub)
	if sub == "" {
		// The token verified but cannot own rows: refuse rather than create a
		// local account that will never converge with the server.
		h.render(w, r, "This token is not bound to an account. Ask the seller for an account token.")
		return errors.New("activation: token not bound")
	}
	if err := h.ensureAccount(r.Context(), sub, claims.Email); err != nil {
		http.Error(w, "activation: account setup failed", http.StatusInternalServerError)
		return err
	}
	if password != "" {
		if perr := h.a.SetPassword(r.Context(), sub, password); perr != nil {
			http.Error(w, "activation: password setup failed", http.StatusInternalServerError)
			return perr
		}
	}
	token, err := h.a.OpenSession(r.Context(), sub)
	if err != nil {
		http.Error(w, "activation: sign-in failed", http.StatusInternalServerError)
		return err
	}
	http.SetCookie(w, h.a.SetSessionCookie(token))
	return nil
}
