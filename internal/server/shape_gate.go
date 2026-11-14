// Runtime page gate for the `api` product shape.
//
// A shape never invents a boot path: `api` is cmd/web with its page routes
// disabled. MountFeature already declines to mount a page route in this shape
// (route.go), so the 404 comes from the tree; this gate is the backstop for a
// path the root registers directly (a leaf mount, a future page route added
// outside the seam). The API, health, auth API and assets keep working.
// Compile-time pruning of the page handlers (and their RBAC rows) remains a
// later optimization, argued in TODO-tradeoffs.md.
package server

import (
	"net/http"
	"strings"
)

// apiPagePrefixes are the surfaces an API-only product still serves. The list
// is deliberately narrow: a new page path is a 404 unless it is added here,
// which is the review moment the shape exists for.
var apiPagePrefixes = []string{
	pathAPI,
	pathHealth,
	pathStatic,
	pathServiceWorker,
	pathUI,
	pathFavicon,
	pathManifest,
	pathRobots,
}

// The paths the api shape keeps. Constants so the gate, the license gate and
// the inbox badge can agree without triple-spelling a literal.
const (
	pathAPI           = "/api"
	pathHealth        = "/health"
	pathStatic        = "/static"
	pathServiceWorker = "/sw.js"
	pathUI            = "/_ui"
	pathFavicon       = "/favicon.ico"
	pathManifest      = "/manifest.webmanifest"
	pathRobots        = "/robots.txt"
)

// applyShapeGate installs the page gate when the tree is the API product shape.
// One line at the composition root keeps chi.go under its file-size budget.
func applyShapeGate(r *Router, apiOnly bool) {
	if apiOnly {
		r.Use(apiOnlyPages)
	}
}

// apiOnlyPages answers 404 for a non-API path. It runs after recovery and
// before the route tree, so a page handler never executes in this shape.
func apiOnlyPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !apiPathAllowed(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// apiPathAllowed is the one predicate the gate uses. Exact matches handle the
// root assets; prefix matches require the separator so `/apix` is not `/api`.
func apiPathAllowed(path string) bool {
	for _, allowed := range apiPagePrefixes {
		if path == allowed || strings.HasPrefix(path, allowed+"/") {
			return true
		}
		// The API surface is /api/*; /api itself is not a route but allowing
		// it costs nothing and keeps the rule simple.
		if allowed == pathAPI && path == pathAPI {
			return true
		}
	}
	return false
}
