// System panorama graph (canvas). The page shell owns chrome; this bundle
// owns the canvas island + its static assets.
package graph

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/calionauta/gogogo/web/resources"
)

// AssetPath is the URL subtree StaticHandler serves.
const AssetPath = "/static/graph"

// URL returns the cache-busting URL for one of the feature's assets.
func URL(name string) string {
	return resources.URLFor(StaticFS(), AssetPath, name)
}

// StaticHandler serves the embedded assets under AssetPath.
func StaticHandler() http.Handler {
	return resources.AssetHandler(StaticFS(), AssetPath)
}

//go:embed static/*
var staticEmbed embed.FS

// StaticFS returns an fs.FS for serving graph static assets.
func StaticFS() fs.FS {
	sub, err := fs.Sub(staticEmbed, "static")
	if err != nil {
		panic("graph: missing static directory: " + err.Error())
	}
	return sub
}
