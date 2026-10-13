// SCOPE:feature — HTTP config view (auth-gated, secrets masked)
//
// Same path, same component (shared Page over BuildPageData): any
// logged-in user may view; guests bounce to /login. Nothing secret-shaped
// ever renders raw — the masking lives in the components package, shared
// with the previous stack, so both pages redact identically.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/config/components"
	"github.com/calionauta/gogogo/internal/auth"
)

// Handler serves the config view on chi.
type Handler struct {
	cfg  *config.Config
	auth *auth.Auth
}

// NewHandler builds the chi config handler. Nil deps panic: wire errors
// surface at boot, not per request.
func NewHandler(cfg *config.Config, a *auth.Auth) *Handler {
	if cfg == nil || a == nil {
		panic("config chi: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, auth: a}
}

// Routes returns the chi router with the config view.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/config", h.handleIndex)
	return r
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	sess, err := h.auth.Session(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	email := ""
	if sess.User != nil {
		email = sess.User.Email
	}
	data := components.BuildPageData(h.cfg)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.Page(data, email, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}
