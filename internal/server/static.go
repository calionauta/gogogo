// SCOPE:core — static asset + Service Worker mounts (the one serving path).
//
// Split out of chi.go, which is at the file-sizes gate: the core bundle and
// the Service Worker serving are one subject. Each handler is a named func so
// the root mounts a value instead of an expression.
package server

import (
	"net/http"

	"github.com/calionauta/gogogo/web/resources"
)

// staticHandler serves the embedded core bundle. The asset table is
// content-hashed, so a changed file can never be served from a stale cache
// entry.
func staticHandler() http.Handler {
	return resources.AssetHandler(resources.StaticFS(), "/static")
}

// serviceWorkerHandler serves sw.js from the ROOT URL so its scope is "/"
// (per spec scope defaults to the script's directory): a SW under /static/
// would only cover /static/ and never intercept /api/* mutations — the
// offline "Add button stuck + todo lost" bug. Service-Worker-Allowed is
// belt-and-suspenders for the { scope: '/' } the client registers with.
func serviceWorkerHandler() http.Handler {
	sw := resources.AssetHandler(resources.StaticFS(), "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Service-Worker-Allowed", "/")
		sw.ServeHTTP(w, r)
	})
}
