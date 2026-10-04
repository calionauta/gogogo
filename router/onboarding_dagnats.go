// SCOPE:feature - REMOVE if not using DagNats workflow.
package router

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo-fullstack-template/config"
	"github.com/calionauta/gogogo-fullstack-template/features/todo/handlers"
	"github.com/calionauta/gogogo-fullstack-template/internal/nats"
	"github.com/calionauta/gogogo-fullstack-template/internal/queue"
)

// registerOnboarding wires the DagNats onboarding workflow into the
// PocketBase router. Called from Init when DagNats is enabled at runtime
// (DAGNATS_ENABLED=true). The DagNats engine must already be running (started in
// cmd/web/dagnats.go) so the handler's client can reach its REST API.
func registerOnboarding(
	app *pocketbase.PocketBase,
	q *queue.Queue,
	se *core.ServeEvent,
	broadcaster nats.TodoBroadcaster,
	todoH *handlers.TodoHandler,
	cfg *config.Config,
) {
	// Guarded here (not at the call site) so Init stays a flat list of
	// one call per capability: when disabled, no routes are registered
	// and the console proxy stays unmounted (avoids zombie 502 routes).
	if !cfg.DagNats.Enabled {
		return
	}
	if todoH == nil {
		return
	}
	dagNatsAddr := cfg.DagNats.HTTPAddr
	// DagNats listens on its own port (cfg.DagNats.HTTPAddr, default
	// 127.0.0.1:8090), separate from the app. The handler client
	// targets that addr, which comes from config (single source of truth).
	handlers.RegisterOnboardingRoutes(app, q, dagNatsAddr, se.Router, broadcaster, todoH)

	// Expose the DagNats console through the app origin at /dagnats so it
	// is reachable via the Cloudflare Tunnel (port 8090 is not tunneled).
	mountDagNatsDashboard(se, dagNatsAddr)
}
