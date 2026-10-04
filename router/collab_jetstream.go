// SCOPE:feature - REMOVE if not using CRDT sync via NATS.
package router

import (
	"context"
	"log/slog"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/internal/collab"
	"github.com/calionauta/gogogo/internal/nats"
)

// registerCollabSync wires the Loro CRDT SyncWorker using the shared
// DocStore (from registerWhiteboard). It subscribes to app.sync.> on
// the embedded NATS and persists resolved whiteboard docs to the
// PocketBase "whiteboards" collection. The worker runs in a goroutine
// until the serve event's context ends. No-op if NATS is unavailable.
func registerCollabSync(se *core.ServeEvent, docs *collab.DocStore) {
	nc := nats.Conn()
	if nc == nil {
		return
	}
	persister := collab.NewPocketBasePersister(se.App)
	worker := collab.NewSyncWorker(nc, persister, docs)
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		se.App.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
			cancel()
			return e.Next()
		})
		if err := worker.Run(ctx); err != nil {
			slog.Error("collab sync worker stopped", "error", err)
		}
	}()

	// Ephemeral presence bridge: browser clients subscribe to a whiteboard's
	// cursors via Server-Sent Events at /api/collab/presence/<docID>. The
	// handler subscribes the same app.presence.<docID> NATS subject the
	// desktop edges publish to, so cursors from any edge (including Leaf
	// Node replicas) stream live to the browser. No persistence.
	presenceH := collab.PresenceSSEHandler(nc)
	se.Router.GET("/api/collab/presence/{docID}", func(c *core.RequestEvent) error {
		presenceH(c.Response, c.Request)
		return nil
	})
}
