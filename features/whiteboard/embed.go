// SCOPE:feature — Collaborative whiteboard (Loro CRDT canvas)
// Embedded static assets for the whiteboard feature: rough.min.js (Rough.js
// hand-drawn canvas rendering) and whiteboard.js (Datastar/SSE canvas wiring).
package whiteboard

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/calionauta/gogogo/web/resources"
)

// AssetPath is the URL subtree StaticHandler serves: the feature owns both
// the files and the path they answer on.
const AssetPath = "/static/wb"

// URL returns the cache-busting URL for one of the feature's own assets
// ("/static/wb/whiteboard.js?v=<hash>"). The URL names the bytes, so the
// browser and any CDN in front of the app hold it immutably instead of
// revalidating it on every navigation (see resources.AssetHandler).
func URL(name string) string {
	return resources.URLFor(StaticFS(), AssetPath, name)
}

// StaticHandler serves the embedded assets under AssetPath. The core bundle
// never carries them, and the feature's own trim drops the mount line, so the
// handler travels with the feature instead of sitting in the composition root.
func StaticHandler() http.Handler {
	return resources.AssetHandler(StaticFS(), AssetPath)
}

//go:embed static/*
var staticEmbed embed.FS

// StaticFS returns an fs.FS for serving whiteboard static assets.
// Wrap with http.FS when passing to http.FileServer.
func StaticFS() fs.FS {
	sub, err := fs.Sub(staticEmbed, "static")
	if err != nil {
		panic("whiteboard: missing static directory: " + err.Error())
	}
	return sub
}
