// SCOPE:layer=feature,removal=feature — Shared notes (server-owned Loro Text)
// Embedded static assets for the notes feature: notes.js (textarea op
// wiring + SSE stream). Kept as a static file (not inline in the .templ)
// because datastar-lint rejects templ expressions inside <script> bodies —
// and plain JS objects would trip the same rule.
package notes

import (
	"embed"
	"io/fs"
)

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
