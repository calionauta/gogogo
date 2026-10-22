// SCOPE:feature — Todo MVC example (reference implementation)
// Package components registers the built-in UI skin at init time so the skin
// dispatcher can resolve it at runtime.
//
// One skin ships (DaisyUI). The seam stays — web/skins.Skin{Name, Assets} plus
// this registration point — because it costs a blank import and is what makes
// "add a skin" a docs page instead of a refactor. Visual variants are DaisyUI
// themes (?theme=), not bundles (docs/ui-skins.md).
package components

import (
	_ "github.com/calionauta/gogogo/web/skins/daisyui"
)
