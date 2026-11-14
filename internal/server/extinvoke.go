// Headless invoke endpoint: the live bridge between the registry and a
// running server.
//
// `POST /api/ext/invoke` resolves (id, verb) against the composed
// registry and re-serves the declared route through the same RBAC-wrapped
// handler the browser uses, under the operator's own session (transported
// as the ordinary cookie). The endpoint is fenced like every other
// surface — a session check of its own (401 without a cookie), a `user`
// policy row, and per-route RBAC still enforced by the wrapped handler —
// so "call someone else's route headless" can never beat "call it in the
// UI". Only mutating routes are invocable (recipe.Invoke enforces it).
package server

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/recipe"
)

// extInvokeRequest is the wire shape. params are form fields forwarded
// verbatim to the target handler (which validates them). Unknown JSON
// fields are refused so a typo fails loud instead of being silently
// dropped.
type extInvokeRequest struct {
	ID     string            `json:"id"`
	Verb   string            `json:"verb"`
	Params map[string]string `json:"params,omitempty"`
}

// mountExtInvoke registers POST /api/ext/invoke on the tree: literal
// paths so the RBAC coverage scan sees the route and demands its row.
func mountExtInvoke(r *Router) {
	r.Claim("core", "POST", "/api/ext/invoke")
	r.Post("/api/ext/invoke", extInvokeHandler(r.recipes, func() http.Handler { return r.invokeOut }))
}

// extInvokeHandler serves POST /api/ext/invoke on the composed tree.
// invokeOut resolves the outer (RBAC-wrapped) handler the invoke re-serves
// through; nil while the tree is still being built (a boot-phase error,
// not a request — the root never serves in that state).
func extInvokeHandler(reg *recipe.Registry, invokeOut func() http.Handler) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req extInvokeRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		cookie, cerr := r.Cookie(auth.CookieName)
		if cerr != nil || cookie.Value == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		action, route, rerr := recipe.ResolveInvokeAction(reg, req.ID, req.Verb)
		if rerr != nil {
			http.Error(w, rerr.Error(), http.StatusBadRequest)
			return
		}
		form := url.Values{}
		for k, v := range req.Params {
			form.Set(k, v)
		}
		status, out, ierr := recipe.Invoke(
			r.Context(), invokeOut(), auth.CookieName, cookie.Value, action, route, form)
		if ierr != nil {
			http.Error(w, ierr.Error(), status)
			return
		}
		// The body is the inner handler's output (echo of the operator's
		// own params through the handler's own rendering). text/plain
		// neutralizes browser interpretation of any reflected markup.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		// `out` is our own composed mux's response to the operator's own params
		// (already rendered by the inner handler); text/plain above forces
		// browsers to treat it as inert text.
		_, _ = w.Write(out)
	})
}
