// SCOPE:feature — Shared notes (server-owned Loro Text)
// Embedded static assets for the notes feature: notes.js (textarea op
// wiring + SSE stream). Kept as a static file (not inline in the .templ)
// because datastar-lint rejects templ expressions inside <script> bodies
// and plain JS objects would trip the same rule.
package notes

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/calionauta/gogogo/web/resources"
)

// AssetPath is the URL subtree StaticHandler serves: the feature owns both
// the files and the path they answer on.
const AssetPath = "/static/notes"

// StaticHandler serves the embedded assets under AssetPath. The core bundle
// never carries them, and the feature's own trim drops the mount line, so the
// handler travels with the feature instead of sitting in the composition root.
func StaticHandler() http.Handler {
	return resources.AssetHandler(StaticFS(), AssetPath)
}

//go:embed static/*
var staticEmbed embed.FS

// StaticFS returns an fs.FS for serving notes static assets.
func StaticFS() fs.FS {
	sub, err := fs.Sub(staticEmbed, "static")
	if err != nil {
		panic("notes: missing static directory: " + err.Error())
	}
	return sub
}
