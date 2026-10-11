// SCOPE:feature — Chi todo stream dispatch (record + worker envelopes)
package chi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/todo/components"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/queue"
)

// Envelope types the chi todo stream understands. Record sync uses the
// recordEvent shape; everything else rides queue.Job envelopes exactly
// like the sibling stack, so producers never learn which stack serves.
const (
	jobTypeToast         = "toast"
	jobTypeSuggestResult = "suggest_result"
	jobTypeClients       = "clients"
)

// retryEvent is the parsed payload of a retry / AI-Suggest broadcast.
type retryEvent struct {
	Operation string `json:"operation"`
	Attempt   int    `json:"attempt"`
	Status    string `json:"status"`
	Error     string `json:"error"`
	Raw       []byte `json:"-"`
}

// Signal keys for suggest/retry/stepper state. One spelling per signal,
// shared by the workers below and the stream dispatcher.
const (
	signalSuggestions = "suggestions"
	signalSuggestErr  = "suggestErr"
	signalAiStep      = "aiStep"
	signalAiPending   = "aiPending"
	signalAiPhase     = "aiPhase"

	signalJobSuggest          = "suggest"
	signalJobSuggestSimulated = "suggest_simulated"
	signalJobRetryDemo        = "retry-demo"

	retryStatusSuccess = "success"
)

// dispatchEnvelope routes one hub message: record events re-list scoped,
// job envelopes fan out to their typed renderer. Unknown types merge as
// lastJob (never an error — a foreign producer sharing nothing must
// still not break this stream).
func (h *Handler) dispatchEnvelope(
	ctx context.Context, sse *sdk.ServerSentEventGenerator, owner string, hasOwner bool, payload []byte,
) error {
	if job, ok := asJob(payload); ok {
		//nolint:contextcheck // job renderers are ctx-free by design (signals
		// + DOM patch only; the DB-backed record path below takes ctx).
		if handled, err := h.dispatchJob(sse, job); handled {
			return err
		}
		// A publisher-wrapped record event also decodes as a Job with an
		// unknown type: fall through to the record path below.
	} else {
		// Not a job envelope: try the record shape directly.
		return h.streamRecordEvent(ctx, sse, owner, hasOwner, payload)
	}
	// Unknown job type: it may still be a record event in disguise.
	// Otherwise surface for debugging without breaking the stream.
	if err := h.streamRecordEvent(ctx, sse, owner, hasOwner, payload); err == nil {
		return nil
	}
	return dshelpers.MergeSignals(sse, map[string]any{
		"lastJob": string(payload),
	})
}

// asJob reports whether payload is a typed worker envelope. Record events
// ({event,id,…}) decode into a Job with an empty Type, so they fall
// through to the record path — never through the job switch.
func asJob(payload []byte) (queue.Job, bool) {
	var job queue.Job
	if err := json.Unmarshal(payload, &job); err != nil {
		return queue.Job{}, false
	}
	if job.Type == "" {
		return queue.Job{}, false
	}
	return job, true
}

// dispatchJob renders one typed worker envelope. It takes no context on
// purpose: none of the renderers touch the DB or the network — they only
// merge signals and patch the DOM — so there is no ctx to thread and no
// contextcheck suppression needed at the call sites.
func (h *Handler) dispatchJob(sse *sdk.ServerSentEventGenerator, job queue.Job) (bool, error) {
	switch job.Type {
	case jobTypeToast:
		return true, h.streamToast(sse, job.Payload)
	case "retry":
		return true, h.streamRetry(sse, job.Payload)
	case jobTypeSuggestResult:
		return true, h.streamSuggestResult(sse, job.Payload)
	case jobTypeProgress:
		return true, h.streamProgress(sse, job.Payload)
	case jobTypeClients:
		return true, h.streamClients(sse, job.Payload)
	default:
		return false, nil
	}
}

// streamRecordEvent decodes a bare record event ({event,id,title,done}).
// Exported-shape sibling of streamRecord, which takes the already-decoded
// envelope from the direct-call path.
func (h *Handler) streamRecordEvent(
	ctx context.Context, sse *sdk.ServerSentEventGenerator, owner string, hasOwner bool, payload []byte,
) error {
	var evt recordEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return fmt.Errorf("decode todo event: %w", err)
	}
	if evt.Event == "" {
		return errors.New("decode todo event: no event field")
	}
	return h.applyRecordEvent(ctx, sse, owner, hasOwner, evt)
}

func (h *Handler) streamToast(sse *sdk.ServerSentEventGenerator, payload []byte) error {
	var p struct {
		ToastType string `json:"toastType"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode toast payload: %w", err)
	}
	if p.ToastType == "" {
		p.ToastType = "info"
	}
	return emitToast(sse, p.Message, p.ToastType)
}

// retrySignalFields builds the Datastar signal map for a retry /
// AI-Suggest event. AI Suggest jobs drive a dedicated stepper
// (aiStep/aiPending); the Queue + Retry demo drives demoStep — running
// one never lights the other.
func retrySignalFields(p retryEvent) map[string]any {
	fields := map[string]any{
		"lastRetry":          string(p.Raw),
		"lastRetryOperation": p.Operation,
		"lastRetryStatus":    p.Status,
		"lastRetryAttempt":   p.Attempt,
	}
	isSuggest := p.Operation == signalJobSuggest || p.Operation == signalJobSuggestSimulated
	if isSuggest {
		if p.Status == retryStatusSuccess {
			fields["aiStep"] = 3
			fields["aiPending"] = false
		} else {
			fields["aiStep"] = 2
			fields["aiPending"] = true
		}
		return fields
	}
	fields["demoStep"] = 2
	if p.Status == retryStatusSuccess {
		fields["demoStep"] = 3
	}
	return fields
}

// retryToastMessage returns the user-facing toast text and kind for a
// retry / AI-Suggest event.
func retryToastMessage(p retryEvent) (msg, kind string) {
	verb := p.Operation
	if verb == signalJobSuggestSimulated {
		verb = "suggest (simulated)"
	}
	switch p.Status {
	case retryStatusSuccess:
		return verb + ": completed", retryStatusSuccess
	default:
		msg = fmt.Sprintf("%s: attempt %d failed", verb, p.Attempt)
		if p.Error != "" {
			msg += " (" + p.Error + ")"
		}
		msg += ", retrying…"
		return msg, "warning"
	}
}

func (h *Handler) streamRetry(sse *sdk.ServerSentEventGenerator, payload []byte) error {
	var p retryEvent
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode retry payload: %w", err)
	}
	p.Raw = payload
	isSuggest := p.Operation == signalJobSuggest || p.Operation == signalJobSuggestSimulated
	if err := dshelpers.MergeSignals(sse, retrySignalFields(p)); err != nil {
		return err
	}
	if p.Status == retryStatusSuccess && !isSuggest {
		if err := h.applyTechStep(sse, signalJobRetryDemo, true, ""); err != nil {
			return err
		}
	}
	msg, kind := retryToastMessage(p)
	return emitToast(sse, msg, kind)
}

// applyTechStep merges the shared techstack stepper state. Terminal
// states release the spinner.
func (h *Handler) applyTechStep(sse *sdk.ServerSentEventGenerator, step string, done bool, phase string) error {
	merge := map[string]any{
		"techStep":  step,
		"techDone":  done,
		"techPhase": phase,
	}
	if done {
		merge["suggestPending"] = false
	}
	return dshelpers.MergeSignals(sse, merge)
}

func (h *Handler) streamSuggestResult(sse *sdk.ServerSentEventGenerator, payload []byte) error {
	slog.Info("todo: streamSuggestResult called", "payload", string(payload))
	var p struct {
		Suggestions    []string `json:"suggestions"`
		SuggestErr     string   `json:"suggestErr"`
		SuggestPending bool     `json:"suggestPending"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode suggest_result payload: %w", err)
	}
	phase := ""
	if p.SuggestErr != "" {
		phase = "error"
	}
	merge := map[string]any{
		signalSuggestions: p.Suggestions,
		signalSuggestErr:  p.SuggestErr,
		signalAiStep:      3,
		signalAiPending:   false,
		signalAiPhase:     phase,
	}
	if err := dshelpers.MergeSignals(sse, merge); err != nil {
		return err
	}
	if err := dshelpers.RenderAndPatch(sse, components.SuggestionsList(p.Suggestions),
		sdk.WithSelector("#suggestions-region")); err != nil {
		return err
	}
	if p.SuggestErr != "" {
		return emitToast(sse, "Suggest failed: "+p.SuggestErr, "error")
	}
	return emitToast(sse, fmt.Sprintf("Got %d suggestions — see below", len(p.Suggestions)), retryStatusSuccess)
}

func (h *Handler) streamClients(sse *sdk.ServerSentEventGenerator, payload []byte) error {
	var p struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode clients payload: %w", err)
	}
	return dshelpers.MergeSignals(sse, map[string]any{
		"connectedClients": p.Count,
	})
}
