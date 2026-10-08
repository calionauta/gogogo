// SCOPE:feature - REMOVE if not using notes.
package router

import (
	"context"
	"io/fs"
	"log"
	"log/slog"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/notes"
	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/web/resources"
)

// registerNotesStack wires the shared-notes feature with a dedicated SSEHub
// (separate from todo/whiteboard hubs so note-text events never reach other
// clients) and its own DocStore + PocketBase persister ("notes"
// collection). Cross-instance server convergence runs on app.notes.>
// (separate from app.sync.>) so snapshots never mix collections.
// Delete with features/notes/: drop the call line from Init and delete
// this file.
func registerNotesStack(se *core.ServeEvent, _ *queue.Queue, cfg *config.Config) {
	hub := queue.NewSSEHub()
	docs := collab.NewDocStore()
	persister := collab.NewPocketBasePersisterForCollection(se.App, "notes")
	nc := nats.Conn() // may be nil; handler + worker degrade to SSE-only

	// Serve notes static assets (notes.js) so the feature is
	// self-contained: when the package is removed, its static assets go
	// with it. Same exact-route pattern as core static assets.
	staticFS := notes.StaticFS()
	staticAssets := resources.AssetHandler(staticFS, "/static")
	if err := fs.WalkDir(staticFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		route := "/static/" + path
		se.Router.GET(route, func(c *core.RequestEvent) error {
			staticAssets.ServeHTTP(c.Response, c.Request)
			return nil
		})
		return nil
	}); err != nil {
		log.Printf("notes: error walking static assets: %v", err)
	}

	h := notes.New(se.App, hub, persister, docs, nc, cfg)
	h.RegisterRoutes(se)

	if nc == nil {
		return
	}
	worker := collab.NewSyncWorkerForSubject(nc, persister, docs, "app.notes.>")
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		se.App.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
			cancel()
			return e.Next()
		})
		if err := worker.Run(ctx); err != nil {
			slog.Error("notes sync worker stopped", "error", err)
		}
	}()
}
