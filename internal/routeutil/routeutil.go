// SCOPE:layer=infra,removal=core — shared routing helper.
//
// A leaf package: it imports only net/http and the PocketBase core types, so
// any package that has a ServeEvent can use it without creating an import
// cycle.
//
// # Why this exists
//
// Registering a route per method, rather than with Router.Any(), is a
// correctness requirement in this codebase, not a style preference.
// Router.Any() registers the route with an EMPTY method, which becomes a
// method-less pattern in Go 1.22+ ServeMux. A method-less pattern conflicts
// with a method-scoped one unless one strictly subsumes the other, and the
// app's own `GET /` (features/landing) does not subsume
// `/dagnats/{path...}`:
//
//	pattern "GET /" conflicts with pattern "/dagnats/{path...}":
//	GET / matches fewer methods than /dagnats/{path...}, but has a more
//	general path pattern
//
// Go's mux panics at registration time, so the process dies before it serves
// anything and the supervisor restart-loops. That reached production once,
// through the DagNats console proxy, and cost eleven hours of downtime.
//
// Two call sites need this: router/dagnats_proxy_dagnats.go and
// features/credits/routes.go. They cannot share code through the router
// package because router imports features/credits, so a helper there would be
// a cycle. Keeping the method list in one place means a future edit cannot
// fix one site and leave the other able to reintroduce the panic.
package routeutil

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// Methods is the set of HTTP methods registered for a proxied or pass-through
// route. It covers what a browser and an HTTP client actually send: GET/HEAD
// for pages and assets, POST/PUT/PATCH/DELETE for the mutating verbs a console
// or API issues, and OPTIONS for preflight.
//
// Ordered by convention rather than alphabetically so a reviewer reading a
// registration site sees the read methods first.
var Methods = []string{
	http.MethodGet,
	http.MethodHead,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodOptions,
}

// RegisterAll registers the same handler for every method in Methods, under
// each of the given patterns.
//
// Use this instead of Router.Any(). See the package comment for why Any() is a
// startup panic here, not a convenience.
func RegisterAll(
	router *router.Router[*core.RequestEvent],
	patterns []string,
	handler func(*core.RequestEvent) error,
) {
	for _, pattern := range patterns {
		for _, method := range Methods {
			router.Route(method, pattern, handler)
		}
	}
}
