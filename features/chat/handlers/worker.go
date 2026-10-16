// SCOPE:feature — HTTP chat worker (model round-trip off the request path)
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/calionauta/gogogo/features/chat/store"
	comp "github.com/calionauta/gogogo/features/genui/components"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// Message roles. Duplicated (not imported) from the framework handler so
// this package stays framework-free; the strings are the persisted shape.
const (
	roleUser      = "user"
	roleAssistant = "assistant"
)

// handleChatJob persists the user prompt, asks the model, parses the
// answer against the catalog, persists the assistant record, and fans
// the result to the originator.
//
// Delivery targets h.hub (this feature's hub), never the pool's shared
// hub argument — same cross-talk guard as the sibling plugin.
//
// Idempotency: the assistant record reuses idem+"#answer", so a queue
// replay stores one conversation, not two. Errors return nil after
// delivering the error envelope: no pool retry for chat (the llm client
// already retries transient faults; a retry would re-run model latency
// while the tab already shows the error).
func (h *Handler) handleChatJob(ctx context.Context, hub *queue.SSEHub, job queue.Job) error {
	target := h.hub
	if target == nil {
		target = hub
	}
	var p struct {
		Prompt string `json:"prompt"`
		UserID string `json:"userId"`
		Idem   string `json:"idem"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("chat: decode ask payload: %w", err)
	}
	if p.UserID == "" {
		return errors.New("chat: missing userId")
	}
	now := time.Now()
	_ = h.store.Append(ctx, p.UserID, store.Message{
		Owner: p.UserID, Role: roleUser, Text: p.Prompt, IdemKey: p.Idem, Created: now,
	})
	dirs, raw, err := comp.AnswerWithRepair(ctx, h.responder, p.Prompt, comp.ParseDirectives)
	var invalid *comp.ValidationError
	if err != nil && !errors.As(err, &invalid) {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Warn(ctx, "chat: model unavailable", obsArgs...)
		_ = h.store.Append(ctx, p.UserID, store.Message{
			Owner: p.UserID, Role: roleAssistant, Text: "model unavailable",
			IdemKey: p.Idem + "#answer", Created: time.Now(),
		})
		// Provider auth failures carry the key↔base mismatch hint
		// (llm.WithAuthHint) so a split triple reads as actionable,
		// not as a cryptic provider string.
		h.send(ctx, target, job.ClientID, chatEnvelope{Error: "model unavailable: " + llm.WithAuthHint(err).Error()})
		return nil
	}
	// A rejected answer after the bounded repair still degrades: chat's
	// contract is that a message never fails, so the last answer goes
	// through the lenient path, which keeps the prose around the payload
	// and hides the payload itself.
	dirs, fallback := dirs, false
	if invalid != nil {
		//nolint:contextcheck // pure CPU (prose strip); ctx enters at Render.
		dirs, fallback, _ = store.ParseChatAnswer(raw)
		observability.Warn(ctx, "chat: answer degraded to prose",
			append(observability.WithComponent("chat", "feature"),
				"outcome", "degraded")...)
	}
	// Fallback carries the whole raw answer inside its single text_note:
	// Text stays empty so the bubble renders once (text + component with
	// the same content rendered everything twice).
	text := ""
	if !fallback && len(dirs) == 1 && dirs[0].Component == "text_note" {
		if t, ok := dirs[0].Props["text"].(string); ok {
			text = t
		}
	}
	_ = h.store.Append(ctx, p.UserID, store.Message{
		Owner: p.UserID, Role: roleAssistant, Text: text,
		Components: dirs, IdemKey: p.Idem + "#answer", Created: time.Now(),
	})
	h.send(ctx, target, job.ClientID, chatEnvelope{Text: text, Directives: dirs})
	return nil
}

// send delivers one chat envelope to a tab (or broadcasts when the job
// carries no client). The envelope type is "chat" — the stream
// dispatcher routes on it, same shape as every other hub producer.
func (h *Handler) send(ctx context.Context, target *queue.SSEHub, clientID string, env chatEnvelope) {
	body, err := json.Marshal(queue.Job{Type: "chat", Payload: mustJSON(ctx, env)})
	if err != nil {
		obsArgs := append(observability.WithComponent("chat", "feature"), "error", err)
		observability.Warn(ctx, "chat: marshal envelope", obsArgs...)
		return
	}
	if clientID == "" {
		//nolint:contextcheck // SSEHub.Send/Broadcast take no context; worker
		// cancellation flows through the surrounding ctx select.
		target.Broadcast(body)
		return
	}
	target.Send(clientID, body)
}
