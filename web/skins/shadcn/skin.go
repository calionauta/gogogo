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

	"github.com/calionauta/gogogo/web/skins"
)

func init() {
	slog.Info("skins: registering shadcn")
	skins.Register(skins.Skin{
		Name:   "shadcn",
		Assets: assets,
	})
}
