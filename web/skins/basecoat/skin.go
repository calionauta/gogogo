// Package basecoat implements the BasecoatUI (shadcn) skin. Uses
// shadcn-style classes directly — no translation layer.
//
// The CSS is built from src/css/basecoat-input.css (imports Tailwind
// + shadcn-inspired semantic classes) and embedded via //go:embed.
package basecoat

import (
	"log/slog"

	"github.com/calionauta/gogogo/web/skins"
)

func init() {
	slog.Info("skins: registering basecoat")
	skins.Register(skins.Skin{
		Name:   "basecoat",
		Assets: assets,
	})
}
