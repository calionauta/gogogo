// SCOPE:plugin — HTTP genui worker (model round-trip off the request path)
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	sdk "github.com/starfederation/datastar-go/datastar"

	comp "github.com/calionauta/gogogo/features/genui/components"
	ic "github.com/calionauta/gogogo/internal/components"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/llm"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
)

// genuiPayload is the worker → tab envelope: rendered directives or an
// error message. Decoded by the stream dispatcher, never trusted blind.
type genuiPayload struct {
	Directives []comp.Directive `json:"directives,omitempty"`
	Error      string           `json:"error,omitempty"`
}

// handleGenuiJob asks the model and fans the result to the originator.
// Errors return nil after delivering the error result: no pool retry for
// Ask (the llm client already retries transient faults; a retry would
// re-run model latency while the tab already shows the error).
func (h *Handler) handleGenuiJob(ctx context.Context, hub *queue.SSEHub, job queue.Job) error {
	target := h.hub
	if target == nil {
		target = hub
	}
	var p struct {
		Prompt string `json:"prompt"`
		UserID string `json:"userId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("genui: decode ask payload: %w", err)
	}
	dirs, _, err := comp.AnswerWithRepair(ctx, h.responder, p.Prompt, comp.ParseDirectives)
	if err != nil {
		if _, invalid := errors.AsType[*comp.ValidationError](err); invalid {
			// The model answered, but never in catalog shape — even after the
			// bounded repair attempt. Its message names what the model got
			// wrong, which is the only actionable part.
			msg := "Ask failed: could not shape the answer (" + err.Error() + ")"
			observability.Warn(ctx, "genui: ask degraded to the error surface",
				append(observability.WithComponent("genui", "feature"),
					"outcome", "fallback", "err", err.Error())...)
			h.sendResult(ctx, target, job.ClientID, nil, msg)
			h.sendToast(ctx, target, job.ClientID, msg, "error")
			return nil
		}
		// The responder's own error says what to do about it — "no LLM
		// configured (GOAI_API_KEY or SIMULATE_LLM=true)", or the provider's
		// message. Replacing it with a fixed "model unavailable" hid the only
		// actionable part, so a missing key and a dead provider looked
		// identical. err.Error() carries no credentials; the same value is
		// already logged and already sent by the chat slice. Provider auth
		// failures get the key↔base mismatch hint (llm.WithAuthHint).
		msg := "Ask failed: " + llm.WithAuthHint(err).Error()
		h.sendResult(ctx, target, job.ClientID, nil, msg)
		h.sendToast(ctx, target, job.ClientID, msg, "error")
		// Delivered as user-visible result; the pool must not retry Ask,
		// so the worker swallows the error and returns nil.
		return nil
	}
	h.sendResult(ctx, target, job.ClientID, dirs, "")
	h.sendToast(ctx, target, job.ClientID, fmt.Sprintf("Rendered %d components", len(dirs)), "success")
	return nil
}

// sendResult delivers the render payload to one tab (or broadcasts when
// the job carries no client).
func (h *Handler) sendResult(
	ctx context.Context, hub *queue.SSEHub, clientID string, dirs []comp.Directive, errMsg string,
) {
	payload := mustJSON(ctx, genuiPayload{
		Directives: dirs, Error: errMsg,
	})
	body := mustJSON(ctx, queue.Job{Type: "genui", Payload: payload})
	if clientID == "" {
		//nolint:contextcheck // SSEHub.Broadcast takes no context; worker
		// cancellation flows through the surrounding ctx select.
		hub.Broadcast(body)
		return
	}
	hub.Send(clientID, body)
}

// sendToast delivers a toast to one tab (or broadcasts).
func (h *Handler) sendToast(ctx context.Context, hub *queue.SSEHub, clientID, message, kind string) {
	p := mustJSON(ctx, map[string]string{"toastType": kind, "message": message})
	body := mustJSON(ctx, queue.Job{Type: "toast", Payload: p})
	if clientID == "" {
		//nolint:contextcheck // SSEHub.Broadcast takes no context; worker
		// cancellation flows through the surrounding ctx select.
		hub.Broadcast(body)
		return
	}
	hub.Send(clientID, body)
}

// surface as write errors, checked per call.
// dispatchStreamMessage routes one hub envelope to its patch. Two cases:
// "genui" renders components into #genui-result, "toast" appends a
// toast. Anything else is ignored (never an error — a foreign producer
// sharing nothing must still not break this stream).
func (h *Handler) dispatchStreamMessage(sse *sdk.ServerSentEventGenerator, msg []byte) error {
	var job queue.Job
	if err := json.Unmarshal(msg, &job); err != nil {
		return fmt.Errorf("genui: decode stream envelope: %w", err)
	}
	switch job.Type {
	case "genui":
		var p genuiPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return fmt.Errorf("genui: decode result payload: %w", err)
		}
		if err := dshelpers.MergeSignals(sse, map[string]any{
			signalGenuiPending: false,
			signalGenuiError:   p.Error,
		}); err != nil {
			return err
		}
		comps, err := comp.RenderAll(p.Directives)
		if err != nil {
			return err
		}
		return dshelpers.RenderAndPatch(sse, comp.GenuiResult(comps, p.Error, p.Error != ""),
			sdk.WithSelector("#genui-result"),
			sdk.WithViewTransitions())
	case "toast":
		var t struct {
			ToastType string `json:"toastType"`
			Message   string `json:"message"`
		}
		if err := json.Unmarshal(job.Payload, &t); err != nil {
			return fmt.Errorf("genui: decode toast payload: %w", err)
		}
		if t.ToastType == "" {
			t.ToastType = "info"
		}
		return genuiToast(sse, t.Message, t.ToastType)
	default:
		return nil
	}
}

// genuiToast appends a toast to the shared toast container (same
// component the todo feature uses — one toast system per app).
func genuiToast(sse *sdk.ServerSentEventGenerator, message, toastType string) error {
	return dshelpers.RenderAndPatch(
		sse,
		ic.Toast(message, toastType, ic.NewToastID()),
		sdk.WithSelectorID("toast-container"),
		sdk.WithModeAppend(),
	)
}
