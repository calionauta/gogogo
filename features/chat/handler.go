// SCOPE:layer=feature,removal=feature — chat queue worker (persist + fan-out)
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/queue"
)

// Role names for persisted records: one spelling per role, so worker
// and tests never drift (same rule as signal key files).
const (
	roleUser      = "user"
	roleAssistant = "assistant"
)

// Handler runs the conversation path: persist both sides, fan the
// answer to the originator's tab. The queue worker — never the HTTP
// handler — calls the model, because an LLM round-trip never runs
// inline (>50ms rule, same as the genui plugin).
type Handler struct {
	cfg       *config.Config
	q         *queue.Queue
	hub       *queue.SSEHub
	store     MessageStore
	responder Responder
}

// chatEnvelope is the worker → tab payload: rendered answer or error,
// plus the assistant text for the list re-fetch path.
type chatEnvelope struct {
	Text       string `json:"text,omitempty"`
	Error      string `json:"error,omitempty"`
	Directives any    `json:"directives,omitempty"`
}

// handleChatJob persists the user prompt, asks the model, parses the
// answer against the catalog, persists the assistant record, and fans
// the result to the originator.
//
// Delivery targets h.hub (this feature's hub), never the pool's shared
// hub argument — same cross-talk guard as the genui plugin. h.hub nil
// falls back to the argument.
//
// Idempotency: the assistant record reuses idem+"#answer", so an SW
// queue replay stores one conversation, not two. Errors return nil
// after delivering the error envelope: no pool retry for chat (the llm
// client already retries transient faults; a retry would re-run model
// latency while the tab already shows the error).
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
	_ = h.store.Append(ctx, p.UserID, Message{
		Owner: p.UserID, Role: roleUser, Text: p.Prompt, IdemKey: p.Idem, Created: now,
	})
	raw, err := h.responder.Respond(ctx, p.Prompt)
	if err != nil {
		slog.Warn("chat: model unavailable", "error", err)
		_ = h.store.Append(ctx, p.UserID, Message{
			Owner: p.UserID, Role: roleAssistant, Text: "model unavailable",
			IdemKey: p.Idem + "#answer", Created: time.Now(),
		})
		h.send(target, job.ClientID, chatEnvelope{Error: "model unavailable: " + err.Error()})
		return nil
	}
	//nolint:contextcheck // pure CPU constructors (no I/O to cancel); ctx enters at component Render instead
	dirs, _, _ := ParseChatAnswer(raw)
	text := ""
	if len(dirs) == 1 && dirs[0].Component == "text_note" {
		if t, ok := dirs[0].Props["text"].(string); ok {
			text = t
		}
	}
	_ = h.store.Append(ctx, p.UserID, Message{
		Owner: p.UserID, Role: roleAssistant, Text: text,
		Components: dirs, IdemKey: p.Idem + "#answer", Created: time.Now(),
	})
	h.send(target, job.ClientID, chatEnvelope{Text: text, Directives: dirs})
	return nil
}

// send delivers one chat envelope to a tab (or broadcasts when the job
// carries no client). The envelope type is "chat" — the stream
// dispatcher routes on it, same shape as every other hub producer.
func (h *Handler) send(target *queue.SSEHub, clientID string, env chatEnvelope) {
	body, err := json.Marshal(queue.Job{Type: "chat", Payload: mustJSON(env)})
	if err != nil {
		slog.Warn("chat: marshal envelope", "error", err)
		return
	}
	if clientID == "" {
		target.Broadcast(body)
		return
	}
	target.Send(clientID, body)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Warn("chat: marshal job", "error", err)
		return nil
	}
	return b
}
