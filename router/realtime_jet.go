// SCOPE:feature - REMOVE if not using NATS JetStream.
package router

import (
	"context"
	"log/slog"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// newTodoBroadcaster builds the JetStream-backed broadcaster from the
// supplied JetStream context (returned by startNATS) and subscribes it
// to the SSE Hub so every connected client (on any instance) receives
// todo mutations. If js is nil (NATS failed to start) it returns the
// in-memory broadcaster instead.
func newTodoBroadcaster(js nats.JetStreamLike, hub *queue.SSEHub) nats.TodoBroadcaster {
	b := nats.NewTodoBroadcaster(js, hub)
	if sub, ok := b.(interface{ Subscribe(*queue.SSEHub) }); ok {
		sub.Subscribe(hub)
	}
	return b
}

// registerCrudConsumer starts the NATS CRUD consumer in a goroutine.
// It subscribes to app.crud.todo.> and persists received operations
// to PocketBase. No-op when NATS is unavailable (js is nil).
// The consumer runs until the server terminates.
//
// The appName parameter is used to derive the JetStream durable consumer
// name so each project deployment gets its own consumer group, preventing
// stale replays when renaming or redeploying the project.
func registerCrudConsumer(se *core.ServeEvent, js nats.JetStreamLike, appName string) {
	if js == nil {
		return // NATS unavailable
	}
	consumer := nats.NewCrudConsumer(se.App, js, appName)
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		se.App.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
			cancel()
			return e.Next()
		})
		if err := consumer.Run(ctx); err != nil {
			slog.Error("crud consumer stopped", "error", err)
		}
	}()
}
