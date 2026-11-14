// The chat slice mounts itself through the recipe seam.
package handlers

import (
	"context"
	"fmt"

	"github.com/calionauta/gogogo/features/chat/store"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the chat handler and hands its declaration to the seam.
// The panel catalog is installed by the todo Register (which owns the
// provider and mounts in the same bundled pass); this unit only validates
// surfaces and builds the handler. Boot errors panic — a composition-root
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
	chatRouter := NewRouter(d.Cfg)
	memStore := store.NewMemStore()
	h := NewHandler(d.Cfg, d.Auth, d.Queue, hub,
		memStore, ResponderFromLLM(d.RealLLM, chatRouter, chatSurfaces))
	if d.ChatRef != nil {
		d.ChatRef.Count = func(ctx context.Context, owner string) (int, int, error) {
			msgs, err := memStore.List(ctx, owner)
			if err != nil {
				return 0, 0, err
			}
			human := 0
			for _, m := range msgs {
				if m.Role == "user" { //nolint:gocritic // chat message role (user/assistant), not an RBAC role
					human++
				}
			}
			return len(msgs), human, nil
		}
	}
	if d.Panels != nil {
		*d.Panels = append(*d.Panels, NewChatPanel(memStore))
	}
	h.SetModelLeg(d.RealLLM, chatRouter, chatSurfaces)
	h.RegisterHandlers(d.Queue.Registry())
	reg.Add(h.Recipe())
}
