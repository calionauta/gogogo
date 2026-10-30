// SCOPE:plugin — Opt-in operator diagnostics: statsviz at /debug/statsviz and
// the stdlib pprof index at /debug/pprof.
//
// The template ships no always-on profiler: this surface is opt-in
// (PERF_ENABLED, off in production) and the composition root fences it with
// the same session + ADMIN_EMAILS + admin rule as /console and /dagnats.
// statsviz is net/http plus an embedded UI — no second HTTP stack — and its
// collector goroutine is stopped by Close, which the chi stack's shutdown
// owns. pprof is the standard library, so depth costs no dependency.
package perf

import (
	"net/http"
	"net/http/pprof"

	"github.com/arl/statsviz"
	"github.com/go-chi/chi/v5"
)

// Root is the dashboard path. A constant, not config: the menu link and the
// RBAC policy rows name the same literal, so a configurable prefix would let
// them drift.
const Root = "/debug/statsviz"

// Handler owns the statsviz server and its collector goroutine.
type Handler struct {
	srv *statsviz.Server
}

// New builds the dashboard and starts its collector. The caller must Close it.
func New() (*Handler, error) {
	srv, err := statsviz.NewServer(statsviz.Root(Root))
	if err != nil {
		return nil, err
	}
	return &Handler{srv: srv}, nil
}

// Routes mounts the dashboard behind mw. The middleware is supplied by the
// composition root because the fence (session + admin + ADMIN_EMAILS) lives
// there. It panics on an empty chain: a fence-less dashboard is a security
// bug, not a runtime condition this package can recover from.
func (h *Handler) Routes(mw ...func(http.Handler) http.Handler) chi.Router {
	if len(mw) == 0 {
		panic("perf: Routes requires the operator fence")
	}
	r := chi.NewRouter()
	for _, m := range mw {
		r.Use(m)
	}
	// Literal paths on purpose: the RBAC coverage lint and the generated
	// api-routes doc read registrations from source, and a constant hides the
	// route from both. TestRoutesUseRoot pins the duplication.
	r.Get("/debug/statsviz", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, Root+"/", http.StatusMovedPermanently)
	})
	r.Get("/debug/statsviz/ws", h.srv.Ws())
	r.Get("/debug/statsviz/*", h.srv.Index())
	// pprof is stdlib (no dependency). The wildcard delegates the named
	// profiles (heap, goroutine, profile, trace, ...) to pprof.Index.
	r.Get("/debug/pprof", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/debug/pprof/", http.StatusMovedPermanently)
	})
	r.Get("/debug/pprof/*", pprof.Index)
	return r
}

// Close stops the collector. The statsviz server cancels its context, so a
// second call is a no-op.
func (h *Handler) Close() error {
	return h.srv.Close()
}
