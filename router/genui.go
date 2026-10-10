// SCOPE:feature - REMOVE if not using genui.
package router

import (
	"os"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/genui"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/queue"
)

// registerGenuiStack wires the Ask demo with a dedicated SSEHub (separate
// from todo/whiteboard hubs so answer fragments never reach other
// clients) and the queue worker that runs the model leg. No NATS, no
// persistence: answers are ephemeral renders of catalog components.
// Delete with features/genui/: drop the call line from Init and delete
// this file.
func registerGenuiStack(se *core.ServeEvent, q *queue.Queue, cfg *config.Config) {
	hub := queue.NewSSEHub()
	real := llm.New(cfg.GoAI.APIKey)
	var simulated *llm.Client
	if v := os.Getenv("SIMULATE_LLM"); v != "false" {
		simulated = llm.NewSimulated()
	}
	h := genui.New(cfg, q, hub, genui.NewResponder(real, simulated))
	h.RegisterRoutes(se)
	h.RegisterHandlers(q.Registry())
}
