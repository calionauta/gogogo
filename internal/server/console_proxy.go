// SCOPE:core — DagNats console reverse proxy (/console/).
//
// The engine serves its operator UI on its own listener
// (DAGNATS_HTTP_ADDR, loopback-only in prod), which browsers cannot
// reach. This mounts it on the app mux instead: same origin, same
// session cookie, same RBAC policy (admin-only rows in policy.csv).
// Paths pass through untouched — upstream serves /console/ with
// absolute asset paths, so any prefix rewriting would break it.
// Disabled engine (DAGNATS_ENABLED=false): no mount, /console/ 404s.
package server

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
)

// mountConsoleProxy exposes the engine console at /console/ when the
// engine is enabled. Guest fence is RequireSession (401, fail-closed);
// role fence is the outer RBAC middleware (policy.csv: admin-only).
// Upstream on loopback needs no engine-side auth changes (its
// AuthLoopback mode passes loopback clients); the X-Forwarded stamp
// below only takes effect on non-loopback deploys with forwarded-auth
// enabled upstream.
func mountConsoleProxy(mux *http.ServeMux, a *auth.Auth, cfg *config.Config) {
	if !cfg.DagNats.Enabled {
		return
	}
	target := normalizeConsoleAddr(cfg.DagNats.HTTPAddr)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "workflow engine unavailable", http.StatusBadGateway)
		},
	}
	gated := a.RequireSession(forwardConsoleUser(proxy))
	mux.Handle("/console/", gated)
	mux.Handle("/console", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/console/", http.StatusMovedPermanently)
	}))
	// /dagnats/ is the branded alias: same console, DagNats name in the
	// address bar. Upstream owns the /console/ paths (absolute assets,
	// actions and API calls), so the alias strips its prefix and
	// re-roots onto /console/; absolute in-console links still land on
	// /console/ (upstream HTML, not ours to rewrite).
	alias := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.URL.Path = "/console/" + strings.TrimPrefix(
				strings.TrimPrefix(r.In.URL.Path, "/dagnats/"), "/")
			r.Out.URL.RawQuery = r.In.URL.RawQuery
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "workflow engine unavailable", http.StatusBadGateway)
		},
	}
	aliasGated := a.RequireSession(forwardConsoleUser(alias))
	mux.Handle("/dagnats/", aliasGated)
	mux.Handle("/dagnats", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dagnats/", http.StatusMovedPermanently)
	}))
}

// forwardConsoleUser stamps the validated session email for the
// upstream audit. On the default loopback engine listener upstream
// runs AuthLoopback (open, attributed "loopback") and ignores the
// headers — the stamp is for non-loopback deploys where the operator
// enabled DAGNATS_CONSOLE_TRUST_FORWARDED_AUTH. RequireSession runs
// first, so a missing session here is impossible — empty email
// degrades to an unattributed console hit, never to a bypass (the
// gate already passed).
func forwardConsoleUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if email := sessionEmail(r); email != "" {
			r.Header.Set("X-Forwarded-User", email)
			r.Header.Set("X-Forwarded-Email", email)
		}
		next.ServeHTTP(w, r)
	})
}

// sessionEmail reads the email RequireSession stored in the context.
func sessionEmail(r *http.Request) string {
	sess, err := auth.SessionFrom(r.Context())
	if err != nil || sess == nil || sess.User == nil {
		return ""
	}
	return sess.User.Email
}

// normalizeConsoleAddr gives a bare host:port its http:// scheme so
// callers pass DAGNATS_HTTP_ADDR verbatim (same rule as the dagnats
// client constructor).
func normalizeConsoleAddr(addr string) *url.URL {
	addr = strings.TrimRight(strings.TrimSpace(addr), "/")
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return &url.URL{Scheme: "http", Host: addr}
	}
	return u
}
