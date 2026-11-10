// The skinned 403: what a signed-in user gets when the policy refuses.
//
// The refusal itself lives in internal/rbac (the policy decides), but the
// ANSWER is a page: the same shell every module renders, with one sentence
// and the two destinations that always work. The middleware cannot import
// internal/ui (the shell imports rbac), so the composition root injects this
// renderer — one seam, no cycle.
//
// The page never names what was behind the door: it says "no access", not
// "this exists but is not yours". The status stays 403.
package server

import (
	"net/http"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/rbac"
	"github.com/calionauta/gogogo/internal/ui"
)

// forbiddenPage builds the renderer the RBAC middleware calls for a
// signed-in user the policy refused.
//
// It is built once at boot (the shell reads the same build label the rest
// of the app shows) and renders per request: the session in the request is
// the one the denial happened for.
func forbiddenPage(cfg *config.Config) rbac.DenyPage {
	return func(w http.ResponseWriter, r *http.Request, sess *auth.ValidatedSession) {
		data := ui.ForbiddenData{}
		if cfg != nil {
			data.BuildLabel = cfg.BuildLabel
			data.BuildCommit = cfg.BuildCommit
		}
		if sess != nil {
			data.Role = sess.Role
			if sess.User != nil {
				data.Email = sess.User.Email
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusForbidden)
		//nolint:contextcheck // templ components receive ctx from Render; the generated boundary is invisible to the lint.
		if err := ui.Forbidden(data).Render(r.Context(), w); err != nil {
			// The status is already on the wire: log what broke and let the
			// client render what it got.
			observability.Error(r.Context(), "forbidden: render page",
				append(observability.WithComponent("server", "core"), "error", err)...)
		}
	}
}
