// SCOPE:feature — HTTP todo onboarding stream rendering (wire side only)
//
// This file is what REMAINS of the onboarding slice in the todo package
// after the engine moved to features/onboarding: the wire constants the
// stream consumes, and the two renderers that merge stepper state into SSE
// responses.
//
// The engine (run lifecycle, step math, poll loop, fan-out) lives in
// features/onboarding and knows nothing of this package. The wire strings
// are repeated on both sides as literals rather than shared — the producer
// never imports the consumer, so trimming the engine deletes its package
// and the boot wiring without touching a line here. What stays compiles
// and simply never fires.
package handlers

import (
	"encoding/json"
	"fmt"

	sdk "github.com/starfederation/datastar-go/datastar"

	dshelpers "github.com/calionauta/gogogo/internal/datastar"
)

// Wire constants the todo stream consumes. Each is repeated as a literal in
// features/onboarding, which publishes them — see that package's doc for why
// they are not shared.
const (
	// jobTypeProgress carries onboarding stepper updates to the streams.
	jobTypeProgress = "progress"

	// eventWorkflowCompleted is the synthetic record event the finished run
	// emits: it marks the stepper done and re-lists the example todos the
	// worker created server-side (they never crossed the HTTP path).
	eventWorkflowCompleted = "workflow-completed"

	// onbPhaseFinalize is the terminal phase: streamProgress marks the
	// stepper done when the envelope carries it.
	onbPhaseFinalize = "finalize"

	// onbPhaseWorkflow labels the stepper's tech node while the run advances.
	onbPhaseWorkflow = "workflow"

	// onbStatusCompleted is the terminal status the re-list path reports.
	onbStatusCompleted = "completed"
)

// streamProgress decodes a progress envelope and merges the stepper state.
// The envelope shape is identical to what the engine publishes, so the UI
// cannot tell them apart.
func (h *Handler) streamProgress(sse *sdk.ServerSentEventGenerator, payload []byte) error {
	var p struct {
		Step   int    `json:"step"`
		Total  int    `json:"total"`
		Phase  string `json:"phase"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode progress payload: %w", err)
	}
	completed := p.Phase == onbPhaseFinalize
	if err := h.applyOnboarding(sse, p.Step, p.Total, p.Phase, p.Detail, completed); err != nil {
		return err
	}
	return emitToast(sse, p.Detail, "info")
}

// applyOnboarding centralises the durable-workflow stepper signals. When
// completed, the panel marks done and releases the spinner.
func (h *Handler) applyOnboarding(
	sse *sdk.ServerSentEventGenerator, step, total int, phase, detail string, completed bool,
) error {
	signals := map[string]any{
		"onboardingActive": true,
		"onboardingStep":   step,
		"onboardingTotal":  total,
		"onboardingPhase":  phase,
		"onboardingDetail": detail,
		"techStep":         onbPhaseWorkflow,
	}
	if completed {
		signals["workflowCompleted"] = true
		signals["techDone"] = true
	}
	return dshelpers.MergeSignals(sse, signals)
}
