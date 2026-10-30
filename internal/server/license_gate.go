// SCOPE:core — Distribution gate: blocks the app until an entitlement token
// is active (LICENSE_MODE=license|account). With LICENSE_MODE=none the gate
// is never in the chain and the store is nil.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/calionauta/gogogo/config"
	licensehandlers "github.com/calionauta/gogogo/features/license/handlers"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/license"
)

// bootModes wires the two distribution axes: the synthetic owner for
// AUTH_MODE=open, the activation store for LICENSE_MODE!=none, and the
// /activate mount that store serves. A nil store means "not in the chain".
func bootModes(cfg *config.Config, a *auth.Auth, queries *dbgen.Queries, mux *Router) (*license.Store, error) {
	if cfg.AuthMode == config.AuthOpen {
		u, err := auth.EnsureOpenUser(context.Background(), queries)
		if err != nil {
			return nil, fmt.Errorf("open-auth user: %w", err)
		}
		a.EnableOpen(u)
		slog.Info("server: auth open (no login)", "user", u.ID)
	}
	st, err := bootLicenseStore(cfg)
	if err != nil || st == nil {
		return st, err
	}
	mux.MountFeature("license", licensehandlers.NewHandler(cfg, st, a, queries).Routes())
	return st, nil
}

// bootLicenseStore loads the verification key and any stored token. A stored
// token that fails verification is logged and ignored: the app boots gated,
// never half-open.
func bootLicenseStore(cfg *config.Config) (*license.Store, error) {
	if cfg.LicenseMode == config.LicenseNone {
		return nil, nil
	}
	if strings.TrimSpace(cfg.LicensePublicKey) == "" {
		// Issuer-only deployment (account mode on the server): nothing to
		// verify locally, so there is no gate in the chain.
		return nil, nil
	}
	pub, err := license.ParsePublicKey(cfg.LicensePublicKey)
	if err != nil {
		return nil, err
	}
	st, oerr := license.Open(cfg.LicenseFile, pub)
	if oerr != nil {
		slog.Warn("license: stored token ignored", "path", cfg.LicenseFile, "error", oerr)
	}
	if st.Valid(time.Now()) {
		c := st.Claims()
		slog.Info("server: license active", "mode", cfg.LicenseMode, "sub", c.Sub, "expires", c.ExpiresAt)
	} else {
		slog.Info("server: license required", "mode", cfg.LicenseMode, "activate", "/activate")
	}
	return st, nil
}

// applyLicenseGate puts the gate in the chain only for a shell that is
// activated: the desktop sets LicenseGate, an issuer deploy does not.
func applyLicenseGate(cfg *config.Config, st *license.Store, next http.Handler) http.Handler {
	if st == nil || !cfg.LicenseGate {
		return next
	}
	return licenseGate(st, next)
}

// licenseGate refuses every request until the app is activated. Pages are
// redirected to /activate; APIs and streams get a 403 JSON so a client never
// parses an HTML activation page as data.
//
// Allowlisted: the activation surface itself, health, the service worker and
// static assets (the activation page needs the CSS bundle), plus logout.
func licenseGate(st *license.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if st.Valid(time.Now()) || activationAllowed(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") ||
			strings.Contains(r.Header.Get("Accept"), "text/event-stream") ||
			r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":  "activation required",
				"action": "/activate",
			})
			return
		}
		http.Redirect(w, r, "/activate", http.StatusSeeOther)
	})
}

func activationAllowed(path string) bool {
	switch path {
	case "/activate", "/health", "/sw.js", "/favicon.ico", "/logout":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}
