// The chat slice mounts itself through the recipe seam.
package handlers

import (
	"fmt"

	"github.com/calionauta/gogogo/features/chat/store"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the chat handler and hands its declaration to the seam.
// The panel catalog arrives via Deps.Panels (built by the root from the
// todo panel, keeping this unit free of a cross-feature import). Boot
// errors (bad surfaces, registry install) panic — a composition-root
// failure must fail the boot, not mount a half-wired answer engine.
func Register(reg *recipe.Registry, d register.Deps) {
	if d.Queue == nil {
		// No queue: the worker cannot run and every ask would 500.
		// Register nothing, same nil-Provider rule as room.
		return
	}
	hub := d.Hub
	if hub == nil {
		hub = queue.NewSSEHub()
	}
	chatSurfaces, err := comp.ParseSurfaces(d.Cfg.GenUI.ChatSurfaces, comp.DefaultSurfacesChat)
	if err != nil {
		panic(fmt.Sprintf("config: GENUI_CHAT_SURFACES: %v", err))
	}
	if verr := comp.ValidateSurfaces(chatSurfaces); verr != nil {
		panic(verr)
	}
	// Deps.Panels carries comp.PanelProvider values as any so the
	// contract omits the genui import (a genui trim would otherwise
	// strip this file). Assert here, where comp is imported anyway.
	panels := make([]comp.PanelProvider, 0, len(d.Panels))
	for _, v := range d.Panels {
		p, ok := v.(comp.PanelProvider)
		if !ok {
			continue
		}
		panels = append(panels, p)
	}
	if err := comp.InstallPanelRegistry(panels); err != nil {
		panic(err)
	}
	var actionProviders []comp.ActionProvider
	for _, p := range panels {
		if ap, ok := p.(comp.ActionProvider); ok {
			actionProviders = append(actionProviders, ap)
		}
	}
	if err := comp.InstallActionRegistry(actionProviders); err != nil {
		panic(err)
	}
	chatRouter := NewRouter(d.Cfg)
	h := NewHandler(d.Cfg, d.Auth, d.Queue, hub,
		store.NewMemStore(), ResponderFromLLM(d.RealLLM, d.SimulatedLLM, chatRouter, chatSurfaces))
	h.SetModelLeg(d.RealLLM, d.SimulatedLLM, chatRouter, chatSurfaces)
	h.RegisterHandlers(d.Queue.Registry())
	reg.Add(h.Recipe())
}
