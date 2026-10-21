// SCOPE:core — Route-table convention: features own their paths, the root
// owns placement, and nothing is written twice.
//
// Every feature exposes `Routes() chi.Router` with ABSOLUTE patterns: the
// todo feature answers /api/todos and /todo from its own router. The root does
// not re-declare those URLs: MountFeature walks the feature router and
// registers each route, with its method and its middleware chain, directly on
// the root tree. That one move is what makes the composition DRY instead of a
// second copy of the URL space:
//
//   - one fact, one place. A route added to a feature is reachable with no
//     edit at the root, so "registered but unreachable" cannot happen the way
//     a hand-maintained mount pair allowed.
//   - convention over configuration. The rule is mechanical (flatten the
//     router). No per-feature mount struct, no table, no registry, no init
//     magic; a new capability is one line at the composition root.
//   - SOLID. A feature answers its own paths, including the URL space it
//     occupies; the root wires it in one line and never restates a route.
//     chi's prefix-stripping Mount is deliberately NOT used: stripping
//     rewrites the path, which is exactly what a hand-rolled handler that
//     parses r.URL.Path cannot survive.
//
// Two safety properties come with it: a duplicate method+pattern PANICS at
// boot with both owners named, and a method the tree does not know for a
// known path answers 405 from the router instead of a hand-rolled check
// inside every handler.
package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5"

	todohandlers "github.com/calionauta/gogogo/features/todo/handlers"
)

// routeKey identifies one registration: a method on a pattern.
type routeKey struct {
	method  string
	pattern string
}

// routeTable is the duplicate guard behind MountFeature: the root tree knows
// every route it serves, so two owners of one method+pattern is a boot error
// naming both — not a silently overwritten handler.
type routeTable struct {
	owner map[routeKey]string
}

// claimRoute records method+pattern as owned by owner, panicking when another
// capability already claimed it. chi silently overwrites a duplicate; this
// names both owners.
func claimRoute(t *routeTable, owner, method, pattern string) {
	key := routeKey{method: method, pattern: pattern}
	if prev, ok := t.owner[key]; ok {
		panic(fmt.Sprintf(
			"server: %s %s registered twice (by %s and %s) — one capability owns a path",
			method, pattern, prev, owner))
	}
	t.owner[key] = owner
}

// Router is the composed app tree: a chi router plus the record of which
// capability claimed each route, so a duplicate is a boot error naming both
// owners. It satisfies chi.Router (embedded), which is what tests and the
// http.Server need; only the composition root uses the extra method.
type Router struct {
	chi.Router
	routes *routeTable
}

// NewRouter builds an empty app tree with its own duplicate-guard record.
func NewRouter() *Router {
	return &Router{Router: chi.NewRouter(), routes: &routeTable{owner: map[routeKey]string{}}}
}

// Claim records method+pattern as owned by owner (used for the handful of
// routes the root registers itself, like /health).
func (r *Router) Claim(owner, method, pattern string) {
	claimRoute(r.routes, owner, method, pattern)
}

// MountFeature registers every route h declares on the tree, keeping the
// method, the pattern and the feature router's own middleware chain. owner
// names the capability in a duplicate-registration panic. A nil router is a
// no-op, so a capability whose engine is disabled (and whose Routes() answers
// nil) mounts nothing instead of panicking at boot.
func (r *Router) MountFeature(owner string, h chi.Router) {
	if h == nil {
		return
	}
	err := chi.Walk(h, func(method, pattern string, handler http.Handler, mws ...func(http.Handler) http.Handler) error {
		r.Claim(owner, method, pattern)
		r.Method(method, pattern, chi.Chain(mws...).Handler(handler))
		return nil
	})
	if err != nil {
		// Walk only fails when the callback does, and the callback cannot.
		// Keep the branch so a future walk error is loud instead of swallowed.
		panic(fmt.Sprintf("server: walk %s routes: %v", owner, err))
	}
}

// MountPath registers h at exactly one path, for any method — the shape for a
// leaf handler that parses its own request (a stream, a redirect).
func MountPath(r chi.Router, path string, h http.Handler) {
	r.Handle(path, h)
}

// MountSubtree registers h at prefix and prefix+"/*" as a plain handler, not
// as a chi subrouter: h sees the full request path in r.URL.Path and in the
// route context, which is what a stdlib mux, a reverse proxy or an asset
// handler that trims its own prefix needs.
func MountSubtree(r chi.Router, prefix string, h http.Handler) {
	r.Handle(prefix, h)
	r.Handle(prefix+"/*", h)
}

// healthHandler answers the scratch image's `app health` probe (a GET on
// 127.0.0.1:PORT) and the Playwright smoke, both of which match the body.
func healthHandler(todoH *todohandlers.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		// The build tag is what a tab open across a deploy compares against
		// (stale-tab reload). The body stays "ok". no-store: an intermediary
		// caching this response would serve a stale tag and fake a mismatch
		// the other way (see session.js buildCheck).
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Gogogo-Build", todoH.BuildLabel+"-"+todoH.BuildCommit)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

// recoveryMiddleware turns a panicking handler into a 500 with a log line
// instead of a dropped connection, so a panic inside a streaming SSE handler
// does not kill the response mid-stream with nothing in the log pointing at
// the handler.
func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler { //nolint:errorlint // the net/http sentinel is a panic value, compared on purpose
					panic(rec) // the net/http contract: abort the response silently
				}
				slog.Error("server: handler panic", "method", r.Method,
					"path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
