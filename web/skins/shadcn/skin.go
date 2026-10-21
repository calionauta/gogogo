// Package shadcn implements the shadcn skin: a token-driven restyle of the
// shared templates over Tailwind CSS, with no component-source dependency.
//
// The CSS is built from src/css/shadcn-input.css and embedded via //go:embed
// in web/resources. Templates keep their semantic class vocabulary (btn,
// card, badge, input, tabs, table, modal, steps, ...), so DaisyUI and
// Basecoat keep working from the same tree.
package shadcn

import (
	"log/slog"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/web/skins"
)

func init() {
	slog.Info("skins: registering shadcn")
	skins.Register(skins.Skin{
		Name:   "shadcn",
		Assets: assets,
	})
}

// assets returns the <link>/<script> elements for the skin. chrome.css (the
// shared top bar) and iconify arrive through authviews.ThemeHead, which every
// page already renders, so they are intentionally not repeated here.
func assets() templ.Component {
	return templ.Raw(`
<link rel="stylesheet" href="/static/shadcn.min.css"/>
<link rel="stylesheet" href="/static/app.css"/>
<script defer type="module" src="/static/theme.js"></script>
<script defer type="module" src="/static/datastar.js"></script>
	`)
}
