// The notes slice mounts itself through the recipe seam.
package handlers

import (
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the notes handler and hands its declaration to the seam.
// The composition root supplies the shared Persister/DocStore/Presence; nil
// fields fall back to local construction so a test that only names Cfg/Auth/
// Queries still mounts the route.
func Register(reg *recipe.Registry, d register.Deps) {
	hub := d.Hub
	if hub == nil {
		hub = queue.NewSSEHub()
	}
	persister := d.Persister
	if persister == nil {
		persister = collab.NewMemoryPersister()
	}
	docs := d.DocStore
	if docs == nil {
		docs = collab.NewDocStore()
		docs.SetPersister(persister)
	}
	h := NewHandler(d.Queries, d.Auth, hub, persister, docs, d.Cfg)
	h.SetPresence(d.Presence)
	reg.Add(h.Recipe())
}
