package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/danmestas/dagnats/server"
	"github.com/danmestas/dagnats/worker"

	"github.com/pocketbase/pocketbase"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/todo/handlers"
	appdagnats "github.com/calionauta/gogogo/internal/dagnats"
	appnats "github.com/calionauta/gogogo/internal/nats"
)

// waitStep waits for d, returning early if ctx is cancelled first. Used for the
// onboarding steps' human-visible pacing: the pause must never outlive a
// shutdown, which a time.Sleep cannot honour.
func waitStep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ctxForStep joins the two cancellation sources an onboarding step handler must
// honour: the pool lifecycle (shutdown) and the engine's step context (step
// abort).
//
// They are NOT the same thing here. DagNats derives the step context from the
// message's trace headers — `observe.ExtractTraceContext` falls back to
// context.Background() — so it carries trace propagation only and is NOT
// cancelled when the server stops. Using it alone would make the pacing pause
// uncancellable again, which is the bug this whole change set removes.
//
// context.AfterFunc (1.21+) propagates cancellation without leaking a
// goroutine per step and without the parent's cancel func escaping, which
// `containedctx` rightly objects to storing.
func ctxForStep(lifecycle context.Context, step worker.TaskContext) context.Context {
	stepCtx := step.Context()
	if stepCtx == nil {
		return lifecycle
	}
	ctx, cancel := context.WithCancel(stepCtx)
	stop := context.AfterFunc(lifecycle, cancel)
	context.AfterFunc(ctx, func() { stop() })
	return ctx
}

// createOnboardingTodo is the onboarding-create-todo step body, extracted so
// the context it persists with is an explicit parameter rather than something
// the handler closure sources invisibly. It writes through the store, which is
// an I/O boundary: the context is what makes that write cancellable, so it must
// be a real one (see ctxForStep).
//
// The run input is {"user": ..., "todos": [...]}; greet (root) forwards it and
// every downstream step's Input is its single dependency's output, so each
// create-todo step receives the same shape with whatever todos remain. Titles
// ride this input/output chain rather than step config because the engine's
// live publish path still omits TaskPayload.Config (per-step `metadata` IS
// delivered as of DagNats v0.0.22, but config never was).
func createOnboardingTodo(ctx context.Context, todoH *handlers.TodoHandler, step worker.TaskContext) error {
	var input struct {
		User  string   `json:"user"`
		Todos []string `json:"todos"`
	}
	if len(step.Input()) > 0 {
		_ = json.Unmarshal(step.Input(), &input)
	}
	text := "Onboarding task"
	if len(input.Todos) > 0 {
		text, input.Todos = input.Todos[0], input.Todos[1:]
	}
	// Scoping the example todos to the owner that started the run is what makes
	// them visible in the user's list (the list query filters by owner).
	// Previously owner was hardcoded to "" so the todos were created owner-less
	// and never surfaced.
	if err := todoH.CreateTodoForOnboarding(ctx, text, input.User); err != nil {
		log.Printf("dagnats: create todo failed: %v", err)
		return step.Fail(err)
	}
	// Thread the remaining titles (and owner) forward so the next create-todo
	// step picks up the next example todo.
	out, err := json.Marshal(input)
	if err != nil {
		return step.Fail(err)
	}
	return step.Complete(out)
}

var dagNatsServer *server.Server

const (
	dagnatsNATSPort = 4222     // fixed conventional port — shared with the realtime broadcaster
	dagnatsMaxStore = 10 << 30 // 10 GiB JetStream store cap (required by dagnats)

	// onboardingWorkflowID is the name the onboarding workflow registers
	// under (internal/dagnats/workflow.go, "name": "onboarding"). The
	// bootstrap seed references it so the placeholder trigger points at a
	// workflow that actually exists.
	onboardingWorkflowID = "onboarding"
)

// startDagNats boots the DagNats durable-workflow engine in the same
// binary on its own HTTP port (cfg.DagNats.HTTPAddr, default :8090). It
// registers the onboarding worker handlers (which write example todos to
// the main PocketBase collection) and starts the engine.
//
// Single-NATS convention: DagNats owns the embedded NATS on the
// conventional port 127.0.0.1:4222. When NATS is enabled, the realtime
// broadcaster (cmd/web/nats.go) connects to THIS NATS instead of starting
// its own — one NATS server, two consumers
// (DagNats workflows + JetStream realtime). startDagNats does NOT block:
// it fires Run() in a goroutine and returns. The synchronization point
// is ConnectExisting (called by startNATS right after), which uses
// nats.RetryOnFailedConnect to block until the engine's NATS is
// reachable — no polling loop in our code.
func startDagNats(ctx context.Context, cfg *config.Config, _ *pocketbase.PocketBase, todoH *handlers.TodoHandler) {
	lifecycleCtx := ctx
	if !cfg.DagNats.Enabled {
		return
	}

	srv := appdagnats.NewServer(cfg.DagNats.StoreDir, cfg.DagNats.HTTPAddr,
		dagnatsNATSPort, dagnatsMaxStore,
	)
	dagNatsServer = srv

	// Register the onboarding worker handlers on the same NATS the engine
	// uses. Handlers are plain functions keyed by task NAME (string), so
	// refactoring Go never orphans an in-flight workflow.
	shim := server.EmbeddedWorker(srv)
	shim.Handle("onboarding-greet", func(ctx worker.TaskContext) error {
		// Pause so a human watching the stepper can read the "greeting" phase.
		// Configurable (DAGNATS_GREET_PACING) because this is product latency,
		// not a neutral demo detail — see config.DagNats.GreetPacing.
		//
		// Waited on a context that carries BOTH cancellation sources: the pool
		// lifecycle (so a shutdown is prompt) and the step context (so the wait
		// ends if the engine aborts the step). The step context alone is not
		// enough — DagNats builds it from the message's trace headers, i.e. from
		// context.Background(), so it is never cancelled on shutdown.
		if err := waitStep(ctxForStep(lifecycleCtx, ctx), cfg.DagNats.GreetPacing); err != nil {
			return ctx.Fail(err)
		}
		log.Printf("dagnats: onboarding greet")
		// Thread the run input ({"user": ..., "todos": [...]}) through as
		// this step's output so the downstream create-todo steps can read
		// the owner + remaining titles from their own Input. Greet is the
		// DAG root, so its Input is exactly the run-level input StartRun
		// received.
		return ctx.Complete(ctx.Input())
	})
	// onboarding-await-first-todo blocks (in-process, on the engine's
	// signal KV) until the app signals "first-todo" — i.e. the user
	// created their first todo. This is the dagnats-documented resume
	// pattern and is how the durable run pauses for external input
	// without polling or an in-memory flag.
	shim.Handle("onboarding-await-first-todo", func(ctx worker.TaskContext) error {
		log.Printf("dagnats: awaiting first todo signal for run %s", ctx.RunID())
		const signalTimeout = 50 * time.Minute
		_, err := ctx.WaitForSignal("first-todo", signalTimeout)
		if err != nil {
			log.Printf("dagnats: await first-todo timed out/failed: %v", err)
			return ctx.Fail(err)
		}
		log.Printf("dagnats: first todo signal received for run %s", ctx.RunID())
		// Pass the owner payload through to the create-todo steps.
		return ctx.Complete(ctx.Input())
	})
	// The closure signature is fixed by worker.HandlerFunc (upstream), so it
	// cannot itself take a context.Context. The work that needs one is a named
	// method, which receives the lifecycle context explicitly — that keeps the
	// dependency visible at the call site instead of hidden in a background
	// context inside the closure.
	shim.Handle("onboarding-create-todo", func(ctx worker.TaskContext) error {
		return createOnboardingTodo(ctxForStep(lifecycleCtx, ctx), todoH, ctx)
	})
	shim.Handle("onboarding-finalize", func(ctx worker.TaskContext) error {
		log.Printf("dagnats: onboarding finalized")
		return ctx.Complete(ctx.Input())
	})

	// Register the onboarding workflow definition idempotently so it is
	// always in sync with this binary. The REST API only comes up once
	// srv.Run() binds the port, so do it in a retry loop that waits for
	// the API to be reachable.
	go registerOnboardingWorkflowWithRetry(ctx, cfg.DagNats.HTTPAddr)

	go func() {
		if err := srv.Run(); err != nil {
			log.Printf("WARN: dagnats server stopped: %v", err)
		}
	}()
	log.Printf("dagnats: listening on %s (NATS on :4222)", cfg.DagNats.HTTPAddr)
}

// registerOnboardingWorkflowWithRetry registers the onboarding workflow,
// retrying until the DagNats REST API is reachable (it boots after
// srv.Run binds the port).
//
// The retry lives in a goroutine, so its context CANNOT be the request's or a
// short-lived one — it is the process lifecycle context from run(). That gives
// two things a context.Background() here did not:
//
//   - a shutdown cancels the in-flight request instead of leaving it to burn its
//     own 10s client timeout, and
//   - the backoff between attempts is a select on that context, so a shutdown is
//     immediate rather than waiting out a bare time.Sleep.
//
// Attempts still cap out (the API may genuinely never come up because
// DAGNATS_ENABLED was flipped, or the port is taken), so this is a bounded
// best-effort registration, not an infinite loop.
func registerOnboardingWorkflowWithRetry(ctx context.Context, httpAddr string) {
	const (
		attempts   = 30
		retryDelay = 500 * time.Millisecond
	)
	client := appdagnats.NewClient("http://" + httpAddr)
	for range attempts {
		if err := client.RegisterWorkflow(ctx, []byte(appdagnats.OnboardingWorkflowJSON)); err != nil {
			if ctx.Err() != nil {
				return // shutting down — not a registration failure
			}
			if waitStep(ctx, retryDelay) != nil {
				return // cancelled during the backoff
			}
			continue
		}
		log.Printf("dagnats: onboarding workflow registered")
		return
	}
	log.Printf("WARN: dagnats workflow register failed after retries")
}

func shutdownDagNats() {
	if dagNatsServer != nil {
		// server.Run blocks until context cancel; the engine's Shutdown
		// is wired internally — closing the process triggers graceful
		// drain via the server's own signal handling.
		dagNatsServer = nil
	}
}

// ensureTriggerBootstrap seeds the engine's trigger KV bucket so the
// trigger console is usable on a fresh install. WORKAROUND — see
// internal/dagnats/trigger_bootstrap.go for the upstream bug this papers
// over, and docs/dagnats-bootstrap-workaround.md for the removal steps.
//
// Deliberately non-fatal: if this fails the trigger UI stays broken, but
// the app must still boot. The wrapped function is a no-op once the bucket
// has any key, so this is cheap on every boot after the first.
func ensureTriggerBootstrap() {
	nc := appnats.Conn()
	if nc == nil {
		log.Printf("dagnats bootstrap: no NATS connection; skipping trigger seed")
		return
	}
	err := appdagnats.EnsureTriggerBucket(nc, onboardingWorkflowID)
	if err != nil {
		log.Printf("WARN: dagnats bootstrap skipped: %v", err)
	}
}
