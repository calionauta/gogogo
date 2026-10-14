// SCOPE:feature — HTTP landing page (public marketing hero)
//
// Same path, same component (shared Index), no auth gate: guests and
// signed-in users see the same hero, with the navbar email filled when a
// session exists.
package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/landing/components"
	"github.com/calionauta/gogogo/internal/auth"
)

// Handler serves the landing page on chi. Auth is optional: a session
// only fills the navbar email, never gates.
type Handler struct {
	cfg  *config.Config
	auth *auth.Auth
}

// NewHandler builds the chi landing handler. Nil deps panic: wire
// errors surface at boot, not per request.
func NewHandler(cfg *config.Config, a *auth.Auth) *Handler {
	if cfg == nil || a == nil {
		panic("landing chi: NewHandler with nil dependency")
	}
	return &Handler{cfg: cfg, auth: a}
}

// Routes returns the chi router with the landing page.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.handleIndex)
	return r
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	email := ""
	role := ""
	if sess, err := h.auth.Session(r); err == nil && sess.User != nil {
		email = sess.User.Email
		role = sess.Role
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = components.Index(email, role, h.cfg.BuildLabel, h.cfg.BuildCommit).Render(r.Context(), w)
}
