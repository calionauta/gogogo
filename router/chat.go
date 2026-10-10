// SCOPE:feature - REMOVE if not using chat.
package router

import (
	"context"
	"os"

	"github.com/pocketbase/pocketbase/core"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/chat"
	"github.com/calionauta/gogogo/features/genui"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/queue"
)

// registerChatStack wires the conversation demo with a dedicated SSEHub
// (separate from genui/todo hubs so answers never reach other clients),
// an in-memory demo store, and the queue worker that runs the model
// leg. History persistence upgrades to PocketBase behind MessageStore
// without touching the worker contract.
// Delete with features/chat/: drop the call line from Init and delete
// this file.
func registerChatStack(se *core.ServeEvent, q *queue.Queue, cfg *config.Config) {
	hub := queue.NewSSEHub()
	real := llm.New(cfg.GoAI.APIKey)
	var simulated *llm.Client
	if v := os.Getenv("SIMULATE_LLM"); v != "false" {
		simulated = llm.NewSimulated()
	}
	h := chat.New(cfg, q, hub, chat.NewMemStore(), chatResponder(real, simulated))
	h.RegisterRoutes(se)
	h.RegisterHandlers(q.Registry())
}

// chatResponder reuses the genui model leg: real key wins, simulated
// key gets the deterministic demo, neither is a fail-closed error.
func chatResponder(real, simulated *llm.Client) chat.Responder {
	return chatResponderAdapter{inner: genui.NewResponder(real, simulated)}
}

// chatResponderAdapter bridges the genui Responder to the chat
// Responder: identical method shape, distinct named types so neither
// feature imports the other's handler.
type chatResponderAdapter struct {
	inner genui.Responder
}

func (a chatResponderAdapter) Respond(ctx context.Context, prompt string) (string, error) {
	return a.inner.Respond(ctx, prompt)
}
