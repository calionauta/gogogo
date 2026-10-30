// SCOPE:core — Mount for the opt-in runtime dashboard (/debug/statsviz).
//
// The dashboard itself is internal/perf (plugin). This file owns the fence:
// session + ADMIN_EMAILS + admin, the same triple /console uses, so the
// surface cannot become public when RBAC_ENABLED=false. Disabled flag: no
// mount, no handler, no collector goroutine.
package server

import (
	"log/slog"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/perf"
	"github.com/calionauta/gogogo/internal/rbac"
)

// bootPerf mounts the runtime dashboard and registers its Close on the
// stack's shutdown. A failure degrades to "no dashboard" (logged) instead of
// failing the boot: it is opt-in observability, not a product surface.
func bootPerf(mux *Router, a *auth.Auth, cfg *config.Config, shutdown *[]func()) *perf.Handler {
	if !cfg.PerfEnabled {
		return nil
	}
	h, err := perf.New()
	if err != nil {
		slog.Warn("server: perf dashboard disabled", "error", err)
		return nil
	}
	mux.MountFeature("perf", h.Routes(
		a.RequireSession,
		operatorGate(cfg.AdminEmails),
		rbac.RequireAdmin(a),
	))
	*shutdown = append(*shutdown, func() { _ = h.Close() })
	return h
}

// closePerf releases the dashboard on a boot failure after bootPerf; the
// shutdown slice that also holds Close never runs on that path.
func closePerf(h *perf.Handler) {
	if h != nil {
		_ = h.Close()
	}
}
