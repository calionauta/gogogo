// SCOPE:core — static asset + Service Worker mounts (the one serving path).
//
// Split out of chi.go, which is at the file-sizes gate: the core bundle and
// the Service Worker serving are one subject, and the per-feature mounts stay
// in chi.go because the installer's trim/add units edit those lines.
package server

import (
	"net/http"

	"github.com/calionauta/gogogo/web/resources"
)

// mountStatic registers the embedded core bundle under /static/ and the
// Service Worker at the ROOT URL. Registered before the landing catch-all so
// the more specific patterns win.
func mountStatic(mux *http.ServeMux) {
	// Exact-prefix ETag handler: the asset table is content-hashed, so a
	// changed file can never be served from a stale cache entry.
	mux.Handle("/static/", resources.AssetHandler(resources.StaticFS(), "/static"))

	// The Service Worker is served from the ROOT URL so its scope is "/"
	// (per spec scope defaults to the script's directory): a SW under
	// /static/ would only cover /static/ and never intercept /api/*
	// mutations — the offline "Add button stuck + todo lost" bug.
	// Service-Worker-Allowed is belt-and-suspenders for the { scope: '/' }
	// the client registers with.
	swAssets := resources.AssetHandler(resources.StaticFS(), "/")
	mux.HandleFunc("/sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Service-Worker-Allowed", "/")
		swAssets.ServeHTTP(w, r)
	})
}
