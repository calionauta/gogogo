// SCOPE:feature — HTTP chat worker (model round-trip off the request path)
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	// Scope the responder to the asker (real data) and carry the recent
	// conversation. Both fix the same reported failure from two sides: the
	// answer invented the user's data because it could not see it, and could not
	// follow a follow-up ("and tomorrow?") because it could not see the thread.
	// The store read happens BEFORE this turn's assistant reply is appended, so
	// the context is the history the user is replying to.
	responder := h.responder
	if scoped, ok := responder.(store.OwnerContext); ok {
		responder = scoped.WithOwner(p.UserID)
	}
	prompt := p.Prompt
	if history := h.recentHistory(ctx, p.UserID); history != "" {
		prompt = history + "\n" + prompt
	}
	_, raw, err := comp.AnswerWithRepair(ctx, responder, prompt, store.ParseChatAnswerForRepair)
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
	// The loop validated through chat's own policy (catalog OR prose); the
	// BUBBLE shape is still chat's: a prose answer is one text_note
	// fallback whose text rides in the component, so Text stays empty and
	// nothing renders twice. ParseChatAnswer is deterministic on the same
	// raw answer.
	//nolint:contextcheck // pure CPU (prose strip); ctx enters at Render.
	dirs, fallback, _ := store.ParseChatAnswer(raw)
	// The app's own panels carry real state, so the worker fills their rows
	// here — the renderer has no store and must not get one. A panel that
	// cannot be hydrated is dropped rather than rendered empty (see
	// components.Hydrate): "you have no todos" would be a lie.
	hydrated, herr := comp.Hydrate(ctx, dirs, p.UserID)
	if herr != nil {
		observability.Warn(ctx, "chat: panel hydration failed",
			append(observability.WithComponent("chat", "feature"), "error", herr.Error())...)
	}
	dirs = hydrated
	if invalid != nil {
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

// historyTurns bounds how many past messages ride the prompt, and
// historyMessageCap how much of each. Both exist because the thread is prompt
// overhead on EVERY request: a long conversation would otherwise crowd out the
// question being asked.
const (
	historyTurns      = 6
	historyMessageCap = 400
)

// recentHistory renders the last few exchanges as prompt context, or "".
//
// WHY: the worker answered every message from the prompt alone, so a follow-up
// ("and tomorrow?", "do the second one") had no referent and the model
// re-invented the subject.
//
// The current user message has already been appended by the time this runs, so
// the tail is dropped: it IS the prompt, and repeating it would double it.
func (h *Handler) recentHistory(ctx context.Context, owner string) string {
	msgs, err := h.store.List(ctx, owner)
	if err != nil {
		observability.Warn(ctx, "chat: history read failed",
			append(observability.WithComponent("chat", "feature"), "error", err.Error())...)
		return ""
	}
	if len(msgs) <= 1 {
		return ""
	}
	msgs = msgs[:len(msgs)-1]
	if len(msgs) > historyTurns {
		msgs = msgs[len(msgs)-historyTurns:]
	}
	var b strings.Builder
	b.WriteString("Recent conversation (oldest first, context only — answer the new message):\n")
	for _, m := range msgs {
		text := strings.TrimSpace(m.Text)
		if text == "" {
			// A components-only answer still counts as a turn, but its content is
			// rendered markup the model cannot read back; naming the shape keeps
			// the thread honest without dumping JSON into the prompt.
			text = "[answered with an interface]"
		}
		if len(text) > historyMessageCap {
			text = text[:historyMessageCap] + "..."
		}
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
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
