// The notes slice mounts itself through the recipe seam.
package handlers

import (
	"github.com/calionauta/gogogo/features/notes"
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
	// Same opaque handle whiteboard takes: register.Deps must not name this
	// servant, or a tree without collab cannot be tidied.
	transport, _ := d.Collab.(collab.Handoff)
	if transport.Persister == nil {
		transport.Persister = collab.NewMemoryPersister()
	}
	if transport.Docs == nil {
		transport.Docs = collab.NewDocStore()
		transport.Docs.SetPersister(transport.Persister)
	}
	h := NewHandler(d.Queries, d.Auth, hub, transport.Persister, transport.Docs, d.Cfg)
	h.SetPresence(transport.Presence)
	reg.Add(h.Recipe())
	// The feature owns its asset subtree too: the root mounts it through the
	// seam so a tree without this slice has no line and no import to trim.
	if d.MountStatic != nil {
		d.MountStatic(notes.AssetPath, notes.StaticHandler())
	}
	if d.Panels != nil {
		*d.Panels = append(*d.Panels, NewNotesPanel(d.Queries))
	}
}
