// SCOPE:plugin — DagNats onboarding workflow (durable run + stepper fan-out)
//
// This package owns the ENGINE side of the onboarding flow: starting the
// durable run against DagNats, polling it, and fanning progress out to an
// SSE hub. It deliberately does NOT own the stream side — decoding progress
// envelopes and merging stepper signals belongs to the feature whose stream
// renders them (features/todo/handlers), because that is wire the stream owns.
//
// The split is what makes the dagnats unit trimmable: deleting this package
// (plus internal/dagnats and the boot wiring) removes the engine without
// touching a line of the todo slice. The todo side keeps its wire constants
// and renderers, which compile and simply never fire — no strip rules
// needed inside features/todo/handlers.
//
// Wire coupling, stated once: the progress envelope this package broadcasts
// is {"step","total","phase","detail"} inside a queue.Job{Type:"progress"},
// and the terminal event is {"event":"workflow-completed",...}. Those
// strings are owned by the todo stream; this package repeats them as
// literals (not imports) so the dependency points the right way — the
// producer never imports the consumer, and trimming the producer cannot
// break the consumer.
package onboarding

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

	ic "github.com/calionauta/gogogo/internal/components"
)

// Phase and status strings. The step math, the poll loop and the published
// envelopes share one spelling here; the todo stream repeats the wire
// values it consumes as its own literals (see the package doc).
const (
	phaseGreet        = "greet"
	phaseWorkflow     = "workflow"
	phaseFinalize     = "finalize"
	phaseError        = "error"
	statusCompleted   = "completed"
	statusFailed      = "failed"
	detailGreet       = "Greeting user"
	detailWaitForTodo = "Waiting for your next to-do (create one to continue)"
	detailFinalize    = "Finalizing onboarding"

	// signalTimeout bounds the signal call to the paused run.
	signalTimeout = 5 * time.Second

	// pollTimeout is the hard ceiling on the polling loop; past it the
	// button re-enables so a wedged run cannot trap the UI.
	pollTimeout = 5 * time.Minute

	// pollInterval paces status reads against the engine REST API.
	pollInterval = 700 * time.Millisecond

	// jobTypeProgress is the worker envelope carrying stepper updates.
	// Must equal the todo stream's dispatch case.
	jobTypeProgress = "progress"

	// greetStep is the 1-based greeting step.
	greetStep = 1

	// awaitStep is the 1-based step that blocks waiting for the user's
	// first todo; the steps after it create the example todos.
	awaitStep = 2

	// exampleTodoCount is how many example todos the workflow creates,
	// matching the titles in dagnats.ExampleTodoTexts.
	exampleTodoCount = 3

	// eventWorkflowCompleted is the synthetic record event the finished run
	// emits. Must equal the todo stream's record-event branch.
	eventWorkflowCompleted = "workflow-completed"
)

// stepOrder is the canonical step order of the onboarding workflow,
// matching the definition uploaded to the engine.
var stepOrder = []string{
	"greet", "await-first-todo", "todo-1", "todo-2", "todo-3", "finalize",
}

// currentStep derives the 1-based current step, the UI phase and a human
// label from the per-step statuses. A "running" step is current; with
// nothing running yet we stay on step 1.
func currentStep(steps map[string]any) (int, string, string) {
	for i, id := range stepOrder {
		st, _ := steps[id].(map[string]any)
		status, _ := st["status"].(string)
		switch status {
		case "running":
			cur := i + 1
			switch {
			case cur <= greetStep:
				return cur, phaseGreet, detailGreet
			case cur == awaitStep:
				return cur, phaseWorkflow, detailWaitForTodo
			case cur < len(stepOrder):
				return cur, phaseWorkflow,
					fmt.Sprintf("Creating example todo %d/%d", cur-awaitStep, exampleTodoCount)
			default:
				return cur, phaseFinalize, detailFinalize
			}
		case statusFailed:
			detail, _ := st["detail"].(string)
			return i + 1, phaseError, detail
		}
	}
	return 1, phaseGreet, detailGreet
}

// failedStep returns the 1-based index and detail of the first failed step,
// defaulting to step 1.
func failedStep(steps map[string]any) (int, string) {
	for i, id := range stepOrder {
		st, _ := steps[id].(map[string]any)
		status, _ := st["status"].(string)
		if status == statusFailed {
			detail, _ := st["detail"].(string)
			return i + 1, detail
		}
	}
	return 1, "unknown error"
}

// recordEvent is the hub payload for workflow-level notifications. Same JSON
// shape as the todo stream's record events ({event,id,title,done}) —
// repeated here rather than imported so trimming this package cannot break
// the consumer.
type recordEvent struct {
	Event string `json:"event"`
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// Handler exposes the DagNats onboarding workflow over HTTP. The engine
// starts the run asynchronously; this handler polls its REST status and
// streams progress to the SSE hub, so the UI stepper advances against the
// REAL run state rather than a sleep countdown.
type Handler struct {
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

// NewHandler builds the handler against the engine's REST base URL, the SSE
// hub it publishes to, and the auth that resolves the owner.
func NewHandler(a *auth.Auth, baseURL string, hub *queue.SSEHub) *Handler {
	return &Handler{
		auth:   a,
		client: dagnats.NewClient(baseURL),
		hub:    hub,
		done:   make(chan struct{}),
	}
}

// Shutdown wakes every poll goroutine. Safe to call repeatedly.
func (h *Handler) Shutdown() {
	h.shutdownOnce.Do(func() {
		if h.done != nil {
			close(h.done)
		}
	})
}

// shutdownCtx bridges the done channel to a context, so poll goroutines
// stop on shutdown without a context stored on the struct.
func (h *Handler) shutdownCtx(parent context.Context) (context.Context, context.CancelFunc) {
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
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/api/onboarding/start", h.handleStart)
	return r
}

// ResumeOnboarding signals the paused run so it resumes creating the
// example todos. Wired from the todo create path: the first todo a user
// adds delivers the "first-todo" signal to the blocked WaitForSignal step.
// It satisfies the todo slice's resumer interface by method shape alone —
// no import in either direction, which is what keeps the trim clean.
func (h *Handler) ResumeOnboarding(_ string) {
	h.mu.Lock()
	runID := h.activeRunID
	h.mu.Unlock()
	if runID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), signalTimeout)
	defer cancel()
	if err := h.client.Signal(ctx, runID, "first-todo", []byte(`{"resumed":true}`)); err != nil {
		slog.Warn("onboarding: signal first-todo failed", "run", runID, "error", err)
		h.broadcastEvent("workflow-error", "resume failed: "+err.Error())
		return
	}
	slog.Info("onboarding: signalled first-todo", "run", runID)
	h.broadcastEvent("workflow-resumed", "First todo captured — workflow resuming")
}

// handleStart kicks off the run and answers the SSE ack immediately. The
// poll loop runs from a goroutine bound to shutdown, never to the request:
// a disconnecting client must not cancel a durable run.
func (h *Handler) handleStart(w http.ResponseWriter, r *http.Request) {
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
		slog.Error("onboarding: start toast failed", "error", err)
	}
}

// ownerID resolves the caller's owner id. Fail-closed: an empty id is an
// error, never a value that reaches an unscoped store call.
func (h *Handler) ownerID(r *http.Request) (string, error) {
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
func (h *Handler) startAndPoll(user string) {
	ctx, cancel := h.shutdownCtx(context.Background())
	defer cancel()
	runID, err := h.client.StartRun(ctx, "onboarding",
		map[string]any{"user": user, "todos": dagnats.ExampleTodoTexts})
	if err != nil {
		slog.Error("onboarding: start failed", "user", user, "error", err)
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
func (h *Handler) pollRun(ctx context.Context, runID string) {
	timeout := time.After(pollTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	total := len(stepOrder)
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
func (h *Handler) applyRunState(
	overall string, steps map[string]any, total int,
	lastStep *int, lastPhase, lastDetail *string,
) bool {
	switch overall {
	case statusCompleted:
		h.publishProgress(total, total, phaseFinalize,
			fmt.Sprintf("Step %d/%d — Onboarding complete", total, total))
		h.broadcastEvent(eventWorkflowCompleted, "")
		return true
	case statusFailed:
		cur, detail := failedStep(steps)
		h.publishProgress(cur, total, phaseError, "Onboarding failed: "+detail)
		h.broadcastEvent("workflow-error", detail)
		return true
	default:
		cur, phase, detail := currentStep(steps)
		if cur == *lastStep && phase == *lastPhase && detail == *lastDetail {
			return false
		}
		*lastStep, *lastPhase, *lastDetail = cur, phase, detail
		h.publishProgress(cur, total, phase,
			fmt.Sprintf("Step %d/%d — %s", cur, total, detail))
		return false
	}
}

// publishProgress merges the stepper signals on every connected stream.
// Local to this instance's hub by design: the poll loop that owns the run
// lives here, and the signal is ephemeral UI state, not a record.
//
// It takes no context on purpose — the only sink is the SSE hub, which
// takes no context — so there is no context to thread and no suppression
// at the boundary.
func (h *Handler) publishProgress(step, total int, phase, detail string) {
	payload, err := json.Marshal(map[string]any{
		"step": step, "total": total, "phase": phase, "detail": detail,
	})
	if err != nil {
		slog.Error("onboarding: marshal progress", "error", err)
		return
	}
	h.hub.Broadcast(mustJSON(queue.Job{Type: jobTypeProgress, Payload: payload}))
}

// broadcastEvent emits a workflow-level notification as a record event so
// every stream marks the stepper done (or failed) and re-lists. Also
// context-free for the same reason as publishProgress.
func (h *Handler) broadcastEvent(event, title string) {
	payload, err := json.Marshal(recordEvent{Event: event, Title: title})
	if err != nil {
		slog.Error("onboarding: marshal event", "error", err)
		return
	}
	h.hub.Broadcast(payload)
}

// mustJSON marshals for job envelopes; unmarshalable input cannot happen
// from the shapes above, so failure is logged, never returned.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		slog.Error("onboarding: marshal envelope", "error", err)
		return []byte("{}")
	}
	return b
}

// writeText answers a plain-text status. Local copy of the todo slice's
// helper: this package must not import the todo slice (that would point the
// trim the wrong way — deleting this package must never break its consumer).
func writeText(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}

// redirectLogin sends unauthenticated callers to the login page.
func redirectLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// emitToast patches a toast into the DOM over the stream. Local copy for
// the same reason as writeText above.
func emitToast(sse *sdk.ServerSentEventGenerator, message, toastType string) error {
	return dshelpers.RenderAndPatch(sse,
		ic.Toast(message, toastType, ic.NewToastID()),
		sdk.WithSelectorID("toast-container"),
		sdk.WithModeAppend(),
	)
}
