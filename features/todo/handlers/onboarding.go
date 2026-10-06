// SCOPE:layer=feature,removal=feature — Todo MVC example (reference implementation)
package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/features/auth"
	"github.com/calionauta/gogogo/internal/dagnats"
	"github.com/calionauta/gogogo/internal/datastar"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// OnboardingHandler exposes the DagNats onboarding workflow over HTTP.
// The workflow runs asynchronously inside DagNats; the HTTP response is a
// "started" toast + signal, and the handler polls the run status and
// streams progress to every connected client via the broadcaster so the
// UI stepper lights up live — DagNats durability + JetStream realtime
// observed together.
type OnboardingHandler struct {
	app         *pocketbase.PocketBase
	client      *dagnats.Client
	broadcaster nats.TodoBroadcaster

	mu          sync.Mutex
	activeRunID string // the run currently awaiting the first-todo signal

	// done is closed on app shutdown. Poll goroutines select on it so they
	// exit promptly; the durable DagNats workflow itself is unaffected. A
	// channel (not a stored context.Context) keeps this struct free of
	// embedded context, matching internal/queue.WorkerPool and the
	// containedctx lint rule.
	done chan struct{}
	// shutdownOnce guards close(done) so a late/duplicate OnTerminate cannot
	// double-close and panic.
	shutdownOnce sync.Once
}

// shutdown closes the done channel exactly once, waking every poll goroutine.
// Safe to call from OnTerminate and from tests.
func (h *OnboardingHandler) shutdown() {
	h.shutdownOnce.Do(func() {
		if h.done != nil {
			close(h.done)
		}
	})
}

// shutdownCtx returns a context cancelled when the app shuts down (or when
// parent is done), plus its cancel func. It is the bridge between the struct's
// done channel and the context-taking client calls, so goroutines stop on
// shutdown without storing a context on the struct. Never returns nil.
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

// shutdownStopped reports whether shutdown has run, so callers (and tests) can
// observe the state without reaching into the channel directly.
func (h *OnboardingHandler) shutdownStopped() bool {
	if h.done == nil {
		return false
	}
	select {
	case <-h.done:
		return true
	default:
		return false
	}
}

// RegisterOnboardingRoutes wires the onboarding HTTP routes into the
// router. Always compiled in the unified build (no build tag); it no-ops
// when DAGNATS_ENABLED=false. The DagNats engine must already be running
// (started in cmd/web/dagnats.go) so the client can reach its REST API
// on baseURL.
func RegisterOnboardingRoutes(
	app *pocketbase.PocketBase,
	_ *queue.Queue,
	baseURL string,
	r *router.Router[*core.RequestEvent],
	broadcaster nats.TodoBroadcaster,
	todoH *TodoHandler,
) {
	if r == nil || broadcaster == nil {
		return
	}
	h := &OnboardingHandler{
		app:         app,
		client:      dagnats.NewClient(baseURL),
		broadcaster: broadcaster,
		done:        make(chan struct{}),
	}
	// Stop in-flight poll goroutines on app termination. Registered once at
	// wiring time (not per request) so the lifetime is the process's: the
	// durable workflow keeps advancing after the client navigates away, but
	// nothing outlives the process.
	if app != nil {
		app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
			h.shutdown()
			return e.Next()
		})
	}
	// Link into TodoHandler so the create path can reach it via the
	// OnboardingResumer interface and resume the durable run when the
	// user creates their first todo.
	todoH.onboarding = h

	r.POST("/api/onboarding/start", h.handleStart).BindFunc(auth.LoadAppAuth)
}

// ResumeOnboarding signals the paused onboarding run so it resumes
// creating the example todos. Wired from the create path: when a user
// with a pending onboarding adds their first todo, this delivers the
// "first-todo" signal to the blocked WaitForSignal step. No-op if no
// run is currently awaiting the signal.
func (h *OnboardingHandler) ResumeOnboarding(_ string) {
	h.mu.Lock()
	runID := h.activeRunID
	h.mu.Unlock()
	if runID == "" {
		slog.Debug("onboarding: resume called but no active run")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), onbSignalTimeout)
	defer cancel()
	if err := h.client.Signal(ctx, runID, "first-todo", []byte(`{"resumed":true}`)); err != nil {
		slog.Warn("onboarding: signal first-todo failed", "run", runID, "error", err)
		if h.broadcaster != nil {
			_ = h.broadcaster.PublishTodoUpdate(ctx,
				todoUpdateJob("workflow-error", "remote", "", "resume failed: "+err.Error(), false))
		}
		return
	}
	slog.Info("onboarding: signalled first-todo", "run", runID)
	if h.broadcaster != nil {
		_ = h.broadcaster.PublishTodoUpdate(ctx,
			todoUpdateJob("workflow-resumed", "remote", "",
				"First todo captured — workflow resuming", false))
	}
}

// publishProgress streams a "progress" event through the broadcaster so
// every connected client sees the durable workflow advance step by step.
func (h *OnboardingHandler) publishProgress(ctx context.Context, step, total int, phase, detail string) {
	if h.broadcaster == nil {
		return
	}
	p := mustJSON(map[string]any{
		"step":   step,
		"total":  total,
		"phase":  phase,
		"detail": detail,
	})
	envelope := mustJSON(queue.Job{Type: "progress", Payload: p})
	if err := h.broadcaster.PublishTodoUpdate(ctx, envelope); err != nil {
		slog.Warn("onboarding: publish progress failed", "error", err)
	}
}

func (h *OnboardingHandler) handleStart(c *core.RequestEvent) error {
	if err := c.Request.ParseForm(); err != nil {
		return c.String(http.StatusBadRequest, "invalid form")
	}
	user := c.Request.FormValue("user")
	if user == "" && c.Auth != nil {
		user = c.Auth.Id
	}
	if user == "" {
		user = "friend"
	}

	// Start the workflow in a goroutine. The pollRun loop within will
	// publish progress events as the workflow advances — no need to
	// publish a synthetic "Step 1/6" here, because the first poll tick
	// detects the greet step and publishes it naturally. Publishing
	// from BOTH places creates a duplicate toast.
	// The run input carries the owner AND the example todo titles. The titles
	// travel in the input (not step config) because DagNats never delivers
	// per-step `config` to workers — the input/output chain is the durable
	// channel, and the root step threads it forward to the create-todo steps.
	//
	// ctx is derived from Background, NOT c.Request.Context(): the durable
	// workflow must survive the client disconnecting. shutdownCtx still ties
	// it to app shutdown so the goroutine cannot outlive the process.
	go func() {
		ctx, cancel := h.shutdownCtx(context.Background())
		defer cancel()
		runID, err := h.client.StartRun(ctx, "onboarding",
			map[string]any{"user": user, "todos": dagnats.ExampleTodoTexts})
		if err != nil {
			slog.Error("onboarding: start failed", "user", user, "error", err)
			if h.broadcaster != nil {
				_ = h.broadcaster.PublishTodoUpdate(ctx,
					todoUpdateJob("workflow-error", "remote", "", err.Error(), false))
			}
			return
		}
		h.mu.Lock()
		h.activeRunID = runID
		h.mu.Unlock()
		h.pollRun(runID)
	}()

	sse := sdk.NewSSE(c.Response, c.Request)
	if err := datastar.MergeSignals(sse, map[string]any{
		"onboardingStarted": true,
		"onboardingUser":    user,
	}); err != nil {
		return err
	}
	return emitToast(sse, "Onboarding started — create a todo to continue", "info")
}

// pollRun watches the DagNats run and streams progress to the UI until
// the run reaches a terminal state, reading DagNats' REST status. It is
// driven by the REAL run state (not a fixed sleep countdown), so the
// stepper reflects what the durable workflow is actually doing — including
// the "waiting for your first todo" suspend, which can last indefinitely
// until the user creates a todo. The button is re-enabled (OnboardingActive
// cleared) on completion, failure, or a hard timeout so the user is never
// trapped in a permanently disabled state.
// onboardingStepOrder is the canonical 1-based order of the onboarding
// workflow steps, matching OnboardingWorkflowJSON. pollRun maps the
// per-step status (from GetRunRaw) to a current-step number so the UI
// advances even while DagNats reports the run's OVERALL status as
// "running" (not "waiting") during the WaitForSignal await step.

// Step vocabulary (onb* constants, step order, step math) lives in
// onboarding_progress.go — deliberately DagNats-free so the installer can
// delete THIS file with the `dagnats` unit while todo_sse.go keeps
// compiling.

//nolint:gocyclo // extracting the completed catch-up loop would add abstraction over single-use sim
func (h *OnboardingHandler) pollRun(runID string) {
	ctx, cancel := h.shutdownCtx(context.Background())
	defer cancel()
	timeout := time.After(onbPollTimeout)
	ticker := time.NewTicker(onbPollInterval)
	defer ticker.Stop()
	total := len(onboardingStepOrder)

	// Track last published state so we don't emit duplicate progress
	// events (and duplicate toasts) on every 700ms poll tick. Each
	// distinct step/phase/detail combo fires exactly one progress event
	// until the state changes — no more toast storms.
	var lastStep int
	var lastPhase, lastDetail string

	for {
		select {
		case <-ctx.Done():
			// App shutting down: stop polling without emitting a
			// misleading "timed out" toast.
			return
		case <-timeout:
			// Give up: re-enable the button so the user isn't stuck.
			h.publishProgress(ctx, 0, 0, "idle", "Onboarding timed out")
			if h.broadcaster != nil {
				_ = h.broadcaster.PublishTodoUpdate(ctx,
					todoUpdateJob("workflow-timeout", "remote", "", "", false))
			}
			return
		case <-ticker.C:
		}

		raw, err := h.client.GetRunRaw(ctx, runID)
		if err != nil {
			// Transient engine error — keep polling unless timed out.
			continue
		}
		overall, _ := raw["status"].(string)
		steps, _ := raw["steps"].(map[string]any)

		switch overall {
		case onbStatusCompleted:
			for s := lastStep + 1; s < total; s++ {
				var phase, detail string
				//nolint:mnd // step numbers 1-6 are structural, not magic
				switch s {
				case 1:
					phase = onbPhaseGreet
					detail = onbDetailGreet
				case 2:
					phase = onbPhaseWorkflow
					detail = onbDetailWaitForTodo
				case 3, 4:
					phase = onbPhaseWorkflow
					detail = fmt.Sprintf("Creating example todo %d/3", s-2)
				case 5:
					phase = onbPhaseWorkflow
					detail = fmt.Sprintf("Creating example todo %d/3", s-2)
				default:
					phase = onbPhaseFinalize
					detail = onbDetailFinalize
				}
				h.publishProgress(ctx, s, total, phase,
					fmt.Sprintf("Step %d/%d — %s", s, total, detail))
			}
			h.publishProgress(ctx, total, total, onbPhaseFinalize,
				fmt.Sprintf("Step %d/%d — Onboarding complete", total, total))
			if h.broadcaster != nil {
				_ = h.broadcaster.PublishTodoUpdate(ctx,
					todoUpdateJob("workflow-completed", "remote", "", "", false))
			}
			return
		case onbStatusFailed:
			cur, detail := onboardingFailedStep(steps)
			h.publishProgress(ctx, cur, total, onbPhaseError,
				"Onboarding failed: "+detail)
			if h.broadcaster != nil {
				_ = h.broadcaster.PublishTodoUpdate(ctx,
					todoUpdateJob("workflow-error", "remote", "", detail, false))
			}
			return
		default:
			// "running" / "waiting" / anything else: derive the current
			// step from the per-step statuses so the await suspend still
			// advances the stepper to "Waiting for your next to-do".
			// Only publish when the state actually changes to avoid
			// spamming toasts on every poll tick.
			cur, phase, detail := onboardingCurrentStep(steps)
			if cur == lastStep && phase == lastPhase && detail == lastDetail {
				continue
			}
			lastStep, lastPhase, lastDetail = cur, phase, detail
			h.publishProgress(ctx, cur, total, phase,
				fmt.Sprintf("Step %d/%d — %s", cur, total, detail))
		}
	}
}
