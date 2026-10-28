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
		// Surfaces rides the job so each tab toggles its own vocabulary.
		// Pointer distinguishes absent (nil = handler default) from all-off
		// (non-nil, empty = prose-only): encoding/json leaves a missing key
		// nil and decodes [] to a non-nil empty slice.
		Surfaces *[]string `json:"surfaces"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("chat: decode chat payload: %w", err)
	}
	if p.UserID == "" {
		return errors.New("chat: missing userId")
	}
	now := time.Now()
	_ = h.store.Append(ctx, p.UserID, store.Message{
		Owner: p.UserID, Role: roleUser, Text: p.Prompt, IdemKey: p.Idem, Created: now,
	})
	effective := h.effectiveSurfaces(ctx, p.Surfaces)
	// Scope the responder to the owner (real data) and carry the recent
	// conversation. Both fix the same reported failure from two sides: the
	// answer invented the user's data because it could not see it, and could not
	// follow a follow-up ("and tomorrow?") because it could not see the thread.
	// The store read happens BEFORE this turn's assistant reply is appended, so
	// the context is the history the user is replying to.
	responder := h.responderFor(effective)
	if scoped, ok := responder.(store.OwnerContext); ok {
		responder = scoped.WithOwner(p.UserID)
	}
	prompt := p.Prompt
	if history := h.recentHistory(ctx, p.UserID); history != "" {
		prompt = history + "\n" + prompt
	}
	// The repair gate is surface-aware: an answer that validates but reaches
	// for a band the tab turned off is a failure the retry can fix (the
	// feedback names it, the model re-answers within the enabled vocabulary).
	// Without this the filter below would silently hollow answers the model
	// thought were good. Pure prose bypasses the gate: the fallback bubble
	// is the safety net, not a model choice, and it renders in every mode
	// (it is what keeps all-three-off a working chat).
	//nolint:contextcheck // pure CPU (parse + filter); ctx enters at Render.
	parseForRepair := func(raw string) ([]comp.Directive, error) {
		return parseSurfaceAware(raw, effective)
	}
	_, raw, err := comp.AnswerWithRepair(ctx, responder, prompt, parseForRepair)
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
	dirs, dropped, fallback, _ := store.ParseChatAnswer(raw)
	if names := comp.PanelNamesIn(dirs); len(names) > 0 {
		// Per-answer stage, CopilotKit-style: the tab already spins, so
		// this names WHAT it waits for (real rows, not the model). The
		// stem is the same one the stored answer will render with, so a
		// later in-place update can address this answer by id.
		h.send(ctx, target, job.ClientID, chatEnvelope{
			Working:  "Loading your data…",
			IDPrefix: store.IDPrefix(store.Message{IdemKey: p.Idem + "#answer"}),
		})
	}
	dirs, fallback = enforceSurfaces(dirs, fallback, effective)
	logChatOutcome(ctx, dirs, dropped, fallback)
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
	text, dirs := singleNoteAsProse(dirs, fallback)
	answer := store.Message{
		Owner: p.UserID, Role: roleAssistant, Text: text,
		Components: dirs, IdemKey: p.Idem + "#answer", Created: time.Now(),
	}
	_ = h.store.Append(ctx, p.UserID, answer)
	// The live patch carries the SAME id stem the stored copy will render with
	// (store.IDPrefix), so a reload rebuilds the page with the ids this patch
	// already used — otherwise a patch aimed at a component would miss after
	// any refresh, and the two renders would disagree about what an id means.
	h.send(ctx, target, job.ClientID, chatEnvelope{
		Text: text, Directives: dirs, IDPrefix: store.IDPrefix(answer),
	})
	return nil
}

// singleNoteAsProse promotes a lone text_note answer to the message text and
// drops the card, so the bubble renders the sentence once. A valid single note
// IS prose; keeping both rendered the same words twice — as the paragraph and
// again under a "Note" heading (reported live, and visible in the transcript
// that motivated this). The fallback keeps its component: it is the
// deterministic path, and it never carries Text at all.
func singleNoteAsProse(dirs []comp.Directive, fallback bool) (string, []comp.Directive) {
	if fallback || len(dirs) != 1 || dirs[0].Component != "text_note" {
		return "", dirs
	}
	text, _ := dirs[0].Props["text"].(string)
	if text == "" {
		return "", dirs
	}
	return text, nil
}

// effectiveSurfaces resolves the job's surface choice, or the handler default
// for old clients. Present-but-empty is prose-only (every band off); absent
// (nil) is the boot default. Unknown names fail closed here: the ask path
// already rejected them, so this is defense in depth, never a user-facing
// error.
func (h *Handler) effectiveSurfaces(ctx context.Context, job *[]string) []comp.Surface {
	def := h.defaultSurfaces
	if def == nil {
		def = comp.DefaultSurfacesChat
	}
	if job == nil {
		return comp.NormalizeSurfaces(def)
	}
	out := make([]comp.Surface, 0, len(*job))
	for _, name := range *job {
		s := comp.Surface(name)
		if !s.Valid() {
			observability.Warn(ctx, "chat: dropping unknown surface from job",
				append(observability.WithComponent("chat", "feature"), "surface", name)...)
			continue
		}
		out = append(out, s)
	}
	return comp.NormalizeSurfaces(out)
}

// parseSurfaceAware is the repair gate with the tab's vocabulary: an answer
// that validates but reaches for a band the tab turned off is a failure the
// retry can fix (the feedback names it, the model re-answers within the
// enabled vocabulary). Without this the filter below would silently hollow
// answers the model thought were good. Pure prose bypasses the gate: the
// fallback bubble is the safety net, not a model choice, and it renders in
// every mode (it is what keeps all-three-off a working chat).
func parseSurfaceAware(raw string, effective []comp.Surface) ([]comp.Directive, error) {
	dirs, err := store.ParseChatAnswerForRepair(raw)
	if err != nil {
		return nil, err
	}
	if fb, _, isFallback, _ := store.ParseChatAnswer(raw); isFallback && len(fb) == 1 {
		return dirs, nil
	}
	if kept := comp.FilterDirectives(dirs, effective); len(kept) != len(dirs) {
		return nil, errors.New("chat: answer used a component group that is turned off")
	}
	return dirs, nil
}

// enforceSurfaces drops final-answer directives outside the enabled
// vocabulary (the repair above already had its chance to re-answer within
// it). The prose fallback bypasses the gate — it is the safety net, not a
// model choice. An answer left with nothing is replaced by an honest line:
// the user turned the bands off, so the fix is one tap away, not a silent
// hole.
func enforceSurfaces(
	dirs []comp.Directive, fallback bool, effective []comp.Surface,
) ([]comp.Directive, bool) {
	if fallback {
		return dirs, fallback
	}
	if kept := comp.FilterDirectives(dirs, effective); len(kept) > 0 {
		return kept, false
	}
	return proseOffFallback(), true
}

// logChatOutcome emits the countable answer-health signal (stdout is
// JSONL). components = what rendered, dropped = directives the lenient
// pass could not render (repair rate per component says whether the prompt
// or the catalog is the thing to fix), fallback = prose degradation,
// diagrams = mermaid renders. Repair attempts already log in
// AnswerWithRepair; this is the render half. Split out of handleChatJob
// for the complexity gate.
func logChatOutcome(ctx context.Context, dirs []comp.Directive, dropped int, fallback bool) {
	diagrams := 0
	for _, d := range dirs {
		if d.Component == "diagram" {
			diagrams++
		}
	}
	observability.Info(ctx, "chat: answer ready",
		append(observability.WithComponent("chat", "feature"),
			"components", len(dirs), "dropped", dropped,
			"fallback", fallback, "diagrams", diagrams)...)
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
