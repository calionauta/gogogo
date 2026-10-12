// SCOPE:feature — Chi genui worker (model round-trip off the request path)
package chi

import (
	"context"
	"encoding/json"
	"fmt"

	sdk "github.com/starfederation/datastar-go/datastar"

	comp "github.com/calionauta/gogogo/features/genui/components"
	ic "github.com/calionauta/gogogo/internal/components"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
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
	raw, err := h.responder.Respond(ctx, p.Prompt)
	if err != nil {
		// The responder's own error says what to do about it — "no LLM
		// configured (GOAI_API_KEY or SIMULATE_LLM=true)", or the provider's
		// message. Replacing it with a fixed "model unavailable" hid the only
		// actionable part, so a missing key and a dead provider looked
		// identical. err.Error() carries no credentials; the same value is
		// already logged and already sent by the chat slice.
		msg := "Ask failed: " + err.Error()
		//nolint:contextcheck // SSEHub.Send/Broadcast take no context; worker
		// cancellation is handled by the surrounding ctx select.
		h.sendResult(target, job.ClientID, nil, msg)
		//nolint:contextcheck // SSEHub.Send/Broadcast take no context; see above.
		h.sendToast(target, job.ClientID, msg, "error")
		//nolint:nilerr // delivered as user-visible result; pool must not retry Ask.
		return nil
	}
	//nolint:contextcheck // pure CPU (catalog parse); ctx enters at component Render instead.
	dirs, err := comp.ParseDirectives(raw)
	if err != nil {
		// Same reasoning as the responder error above: the parse failure's
		// message is what tells you which directive the model got wrong.
		msg := "Ask failed: could not shape the answer (" + err.Error() + ")"
		//nolint:contextcheck // SSEHub.Send/Broadcast take no context; see above.
		h.sendResult(target, job.ClientID, nil, msg)
		//nolint:contextcheck // SSEHub.Send/Broadcast take no context; see above.
		h.sendToast(target, job.ClientID, msg, "error")
		//nolint:nilerr // same contract as above: delivered, not retried.
		return nil
	}
	//nolint:contextcheck // SSEHub.Send/Broadcast take no context; worker
	// cancellation is handled by the surrounding ctx select.
	h.sendResult(target, job.ClientID, dirs, "")
	//nolint:contextcheck // SSEHub.Send/Broadcast take no context; see above.
	h.sendToast(target, job.ClientID, fmt.Sprintf("Rendered %d components", len(dirs)), "success")
	return nil
}

// sendResult delivers the render payload to one tab (or broadcasts when
// the job carries no client).
func (h *Handler) sendResult(hub *queue.SSEHub, clientID string, dirs []comp.Directive, errMsg string) {
	body := mustJSON(queue.Job{Type: "genui", Payload: mustJSON(genuiPayload{Directives: dirs, Error: errMsg})})
	if clientID == "" {
		hub.Broadcast(body)
		return
	}
	hub.Send(clientID, body)
}

// sendToast delivers a toast to one tab (or broadcasts).
func (h *Handler) sendToast(hub *queue.SSEHub, clientID, message, kind string) {
	p := mustJSON(map[string]string{"toastType": kind, "message": message})
	body := mustJSON(queue.Job{Type: "toast", Payload: p})
	if clientID == "" {
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
