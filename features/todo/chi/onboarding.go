// SCOPE:feature — Chi DagNats onboarding slice (durable workflow + stepper)
//
// Mirrors the sibling stack's DagNats onboarding over the chi seam: same
// REST client, same poll loop, same wire shapes (so the Templ components
// and their signals work unchanged), but published to this stack's own
// SSE hub instead of a framework broadcaster.
//
// The step vocabulary below is a chi-local copy on purpose. This package
// cannot import features/todo/handlers (its framework closure would fail
// the flat-stack dependency gate), and the two stacks diverge here: the
// framework stack's progress rides a JetStream broadcaster, this one
// broadcasts straight to its hub. A shared package would need an interface
// neither side wants — duplicating ~60 lines of step math is the cheaper
// trade, and the shapes are pinned by the contract test.
package chi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/dagnats"
	dshelpers "github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/queue"
)

// Phase and status strings the onboarding stepper understands. Extracted so
// the poll loop, the step math and the signal merge share one spelling.
const (
	onbPhaseGreet        = "greet"
	onbPhaseWorkflow     = "workflow"
	onbPhaseFinalize     = "finalize"
	onbPhaseError        = "error"
	onbStatusCompleted   = "completed"
	onbStatusFailed      = "failed"
	onbDetailGreet       = "Greeting user"
	onbDetailWaitForTodo = "Waiting for your next to-do (create one to continue)"
	onbDetailFinalize    = "Finalizing onboarding"

	// onbSignalTimeout bounds the signal call to the paused run.
	onbSignalTimeout = 5 * time.Second

	// onbPollTimeout is the hard ceiling on the polling loop; past it the
	// button re-enables so a wedged run cannot trap the UI.
	onbPollTimeout = 5 * time.Minute

	// onbPollInterval paces status reads against the engine REST API.
	onbPollInterval = 700 * time.Millisecond

	// jobTypeProgress carries onboarding stepper updates to the streams.
	jobTypeProgress = "progress"

	// onbGreetStep is the 1-based greeting step.
	onbGreetStep = 1

	// onbAwaitStep is the 1-based step that blocks waiting for the user's
	// first todo; the steps after it create the example todos.
	onbAwaitStep = 2

	// onbExampleTodoCount is how many example todos the workflow creates,
	// matching the titles in dagnats.ExampleTodoTexts.
	onbExampleTodoCount = 3

	// eventWorkflowCompleted is the synthetic record event the finished
	// run emits: it marks the stepper done and re-lists the example todos
	// the worker created server-side (they never crossed the HTTP path).
	eventWorkflowCompleted = "workflow-completed"
)

// onboardingStepOrder is the canonical step order of the onboarding
// workflow, matching the definition uploaded to the engine.
var onboardingStepOrder = []string{
	"greet", "await-first-todo", "todo-1", "todo-2", "todo-3", "finalize",
}

// onboardingCurrentStep derives the 1-based current step, the UI phase and
// a human label from the per-step statuses. A "running" step is current;
// with nothing running yet we stay on step 1.
func onboardingCurrentStep(steps map[string]any) (int, string, string) {
	for i, id := range onboardingStepOrder {
		st, _ := steps[id].(map[string]any)
		status, _ := st["status"].(string)
		switch status {
		case "running":
			cur := i + 1
			switch {
			case cur <= onbGreetStep:
				return cur, onbPhaseGreet, onbDetailGreet
			case cur == onbAwaitStep:
				return cur, onbPhaseWorkflow, onbDetailWaitForTodo
			case cur < len(onboardingStepOrder):
				return cur, onbPhaseWorkflow,
					fmt.Sprintf("Creating example todo %d/%d", cur-onbAwaitStep, onbExampleTodoCount)
			default:
				return cur, onbPhaseFinalize, onbDetailFinalize
			}
		case onbStatusFailed:
			detail, _ := st["detail"].(string)
			return i + 1, onbPhaseError, detail
		}
	}
	return 1, onbPhaseGreet, onbDetailGreet
}

// onboardingFailedStep returns the 1-based index and detail of the first
// failed step, defaulting to step 1.
func onboardingFailedStep(steps map[string]any) (int, string) {
	for i, id := range onboardingStepOrder {
		st, _ := steps[id].(map[string]any)
		status, _ := st["status"].(string)
		if status == onbStatusFailed {
			detail, _ := st["detail"].(string)
			return i + 1, detail
		}
	}
	return 1, "unknown error"
}

// OnboardingHandler exposes the DagNats onboarding workflow over the chi
// router. The engine starts the run asynchronously; this handler polls its
// REST status and streams progress to this stack's SSE hub, so the UI
// stepper advances against the REAL run state rather than a sleep countdown.
type OnboardingHandler struct {
	auth   *auth.Auth
	client *dagnats.Client
	hub    *queue.SSEHub

	mu          sync.Mutex
	activeRunID string // the run awaiting the first-todo signal, if any

	// done is closed on shutdown. Poll goroutines select on it so they
	// exit promptly; the durable run itself keeps advancing in the engine.
	// A channel (not a stored context) keeps this struct free of embedded
	// context per the containedctx rule.
	done         chan struct{}
	shutdownOnce sync.Once
}

// NewOnboardingHandler builds the handler against the engine's REST base
// URL, the SSE hub it publishes to, and the auth that resolves the owner.
func NewOnboardingHandler(a *auth.Auth, baseURL string, hub *queue.SSEHub) *OnboardingHandler {
	return &OnboardingHandler{
		auth:   a,
		client: dagnats.NewClient(baseURL),
		hub:    hub,
		done:   make(chan struct{}),
	}
}

// Shutdown wakes every poll goroutine. Safe to call repeatedly.
func (h *OnboardingHandler) Shutdown() {
	h.shutdownOnce.Do(func() {
		if h.done != nil {
			close(h.done)
		}
	})
}

// shutdownCtx bridges the done channel to a context, so poll goroutines
// stop on shutdown without a context stored on the struct.
func (h *OnboardingHandler) shutdownCtx(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	if h.done == nil {
		return ctx, cancel
	}
	go func() {
		select {
		case <-h.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// Routes returns the onboarding router. The start route resolves its owner
// from the session cookie, so an anonymous caller is redirected.
func (h *OnboardingHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/api/onboarding/start", h.handleStart)
	return r
}

// ResumeOnboarding signals the paused run so it resumes creating the
// example todos. Wired from the create path: the first todo a user adds
// delivers the "first-todo" signal to the blocked WaitForSignal step.
func (h *OnboardingHandler) ResumeOnboarding(_ string) {
	h.mu.Lock()
	runID := h.activeRunID
	h.mu.Unlock()
	if runID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), onbSignalTimeout)
	defer cancel()
	if err := h.client.Signal(ctx, runID, "first-todo", []byte(`{"resumed":true}`)); err != nil {
		slog.Warn("chi onboarding: signal first-todo failed", "run", runID, "error", err)
		h.broadcastEvent("workflow-error", "resume failed: "+err.Error())
		return
	}
	slog.Info("chi onboarding: signalled first-todo", "run", runID)
	h.broadcastEvent("workflow-resumed", "First todo captured — workflow resuming")
}

// handleStart kicks off the run and answers the SSE ack immediately. The
// poll loop runs from a goroutine bound to shutdown, never to the request:
// a disconnecting client must not cancel a durable run.
func (h *OnboardingHandler) handleStart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeText(w, http.StatusBadRequest, "invalid form")
		return
	}
	user, err := h.ownerID(r)
	if err != nil {
		redirectLogin(w, r)
		return
	}
	if user == "" {
		user = "friend"
	}
	//nolint:contextcheck // the goroutine owns the engine call and its
	// error fan-out; the hub broadcast inside takes no context.
	go h.startAndPoll(user)

	sse := sdk.NewSSE(w, r)
	if err := dshelpers.MergeSignals(sse, map[string]any{
		"onboardingStarted": true,
		"onboardingUser":    user,
	}); err != nil {
		writeText(w, http.StatusInternalServerError, "start failed")
		return
	}
	//nolint:contextcheck // datastar-go's sinks take no context; the SSE
	// generator was built from the request, whose context governs writes.
	if err := emitToast(sse, "Onboarding started — create a todo to continue", "info"); err != nil {
		slog.Error("chi onboarding: start toast failed", "error", err)
	}
}

// ownerID resolves the caller's owner id. Fail-closed: an empty id is an
// error, never a value that reaches an unscoped store call.
func (h *OnboardingHandler) ownerID(r *http.Request) (string, error) {
	sess, err := h.auth.Session(r)
	if err != nil {
		return "", err
	}
	if sess == nil || sess.User == nil || sess.User.ID == "" {
		return "", errors.New("onboarding: empty session")
	}
	return sess.User.ID, nil
}

// startAndPoll starts the run, records its id for ResumeOnboarding and
// polls it to a terminal state. Errors are published as workflow-error so
// the UI releases its button instead of spinning forever.
func (h *OnboardingHandler) startAndPoll(user string) {
	ctx, cancel := h.shutdownCtx(context.Background())
	defer cancel()
	runID, err := h.client.StartRun(ctx, "onboarding",
		map[string]any{"user": user, "todos": dagnats.ExampleTodoTexts})
	if err != nil {
		slog.Error("chi onboarding: start failed", "user", user, "error", err)
		h.broadcastEvent("workflow-error", err.Error())
		return
	}
	h.mu.Lock()
	h.activeRunID = runID
	h.mu.Unlock()
	h.pollRun(ctx, runID)
}

// pollRun streams the engine's run status until it terminates, times out,
// or the process shuts down. Only a genuine state change is published, so
// a 700ms tick never becomes a toast storm.
func (h *OnboardingHandler) pollRun(ctx context.Context, runID string) {
	timeout := time.After(onbPollTimeout)
	ticker := time.NewTicker(onbPollInterval)
	defer ticker.Stop()
	total := len(onboardingStepOrder)
	var lastStep int
	var lastPhase, lastDetail string

	for {
		select {
		case <-ctx.Done():
			return // shutting down: no misleading "timed out" toast.
		case <-timeout:
			//nolint:contextcheck // SSEHub fan-out takes no context; the
			// hub drops on a full buffer, so there is nothing to cancel.
			h.publishProgress(0, 0, "idle", "Onboarding timed out")
			//nolint:contextcheck // same ctx-free SSE fan-out boundary.
			h.broadcastEvent("workflow-timeout", "")
			return
		case <-ticker.C:
		}

		raw, err := h.client.GetRunRaw(ctx, runID)
		if err != nil {
			continue // transient engine error: keep polling until timeout.
		}
		overall, _ := raw["status"].(string)
		steps, _ := raw["steps"].(map[string]any)
		//nolint:contextcheck // same ctx-free SSE fan-out boundary as above.
		if h.applyRunState(overall, steps, total, &lastStep, &lastPhase, &lastDetail) {
			return
		}
	}
}

// applyRunState reacts to the two terminal statuses and returns true when
// polling should stop. The "running"/"waiting" default reports through the
// out-params so the caller can dedupe repeats. Split from pollRun to keep
// both functions small and the state table readable.
//
// It takes no context: both sinks (stepper signals, workflow events) are
// SSE-hub broadcasts, which take none.
func (h *OnboardingHandler) applyRunState(
	overall string, steps map[string]any, total int,
	lastStep *int, lastPhase, lastDetail *string,
) bool {
	switch overall {
	case onbStatusCompleted:
		h.publishProgress(total, total, onbPhaseFinalize,
			fmt.Sprintf("Step %d/%d — Onboarding complete", total, total))
		h.broadcastEvent(eventWorkflowCompleted, "")
		return true
	case onbStatusFailed:
		cur, detail := onboardingFailedStep(steps)
		h.publishProgress(cur, total, onbPhaseError, "Onboarding failed: "+detail)
		h.broadcastEvent("workflow-error", detail)
		return true
	default:
		cur, phase, detail := onboardingCurrentStep(steps)
		if cur == *lastStep && phase == *lastPhase && detail == *lastDetail {
			return false
		}
		*lastStep, *lastPhase, *lastDetail = cur, phase, detail
		h.publishProgress(cur, total, phase,
			fmt.Sprintf("Step %d/%d — %s", cur, total, detail))
		return false
	}
}

// publishProgress merges the stepper signals on every connected stream and
// narrates the step as a toast. It is local to this instance's hub by
// design: the poll loop that owns the run lives here, and the signal is
// ephemeral UI state, not a record.
//
// It takes no context on purpose — the only sink is the SSE hub, which
// takes no context — so there is no context to thread and no suppression
// at the boundary.
func (h *OnboardingHandler) publishProgress(step, total int, phase, detail string) {
	payload, err := json.Marshal(map[string]any{
		"step": step, "total": total, "phase": phase, "detail": detail,
	})
	if err != nil {
		slog.Error("chi onboarding: marshal progress", "error", err)
		return
	}
	h.hub.Broadcast(mustJSON(queue.Job{Type: jobTypeProgress, Payload: payload}))
}

// broadcastEvent emits a workflow-level notification as a record event so
// every stream marks the stepper done (or failed) and re-lists. Also
// context-free for the same reason as publishProgress.
func (h *OnboardingHandler) broadcastEvent(event, title string) {
	payload, err := json.Marshal(recordEvent{Event: event, Title: title})
	if err != nil {
		slog.Error("chi onboarding: marshal event", "error", err)
		return
	}
	h.hub.Broadcast(payload)
}

// streamProgress decodes a progress envelope and merges the stepper state.
// It is the counterpart of the sibling stack's streamProgress: the envelope
// shape is identical, so the UI cannot tell the two apart.
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
