package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/danmestas/dagnats/server"
	"github.com/danmestas/dagnats/worker"

	"github.com/calionauta/gogogo/config"
	notificationhandlers "github.com/calionauta/gogogo/features/notifications/handlers"
	appdagnats "github.com/calionauta/gogogo/internal/dagnats"
	"github.com/calionauta/gogogo/internal/delivery"
)

// onboardingTodoCreator is what the DagNats onboarding worker needs from
// the todo slice: create one row for an owner. One method on purpose, so
// the worker depends on the capability rather than on a concrete handler
// from a particular stack.
type onboardingTodoCreator interface {
	CreateTodoForOnboarding(ctx context.Context, title, owner string) error
}

// reminderDeliverer is what the engine workers need from the notifications
// slice: write one delivery row for an owner. The row carries the dedupe
// guard, so a retried step is a no-op.
type reminderDeliverer interface {
	Deliver(ctx context.Context, d notificationhandlers.Delivery) error
}

// reminderService is the notifications slice the engine wires at boot: the
// deliverer plus the setter that hands it the workflow client. The setter keeps
// the feature free of an internal/dagnats import, so trimming the engine leaves
// no dangling import and the feature degrades to "no reminder form".
type reminderService interface {
	reminderDeliverer
	SetRunStarter(r notificationhandlers.RunStarter)
}

// reminderDelivery reads the run payload the seed step forwarded and builds the
// delivery the shared path expects. It is a named function so startDagNats stays
// under the funlen gate and the JSON shape lives in one place.
func reminderDelivery(raw []byte) (notificationhandlers.Delivery, error) {
	var in struct {
		Seed *struct {
			User string `json:"user"`
			//nolint:tagliatelle // the workflow input uses snake_case.
			TransactionID string            `json:"transaction_id"`
			Body          string            `json:"body"`
			Channels      []string          `json:"channels"`
			Recipients    map[string]string `json:"recipients"`
		} `json:"seed"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return notificationhandlers.Delivery{}, err
	}
	if in.Seed == nil {
		return notificationhandlers.Delivery{}, errors.New("dagnats: reminder input has no seed")
	}
	channels := make([]delivery.Channel, 0, len(in.Seed.Channels))
	for _, ch := range in.Seed.Channels {
		channels = append(channels, delivery.Channel(ch))
	}
	recipients := make(map[delivery.Channel]string, len(in.Seed.Recipients))
	for ch, addr := range in.Seed.Recipients {
		recipients[delivery.Channel(ch)] = addr
	}
	return notificationhandlers.Delivery{
		Owner:         in.Seed.User,
		TransactionID: in.Seed.TransactionID,
		Workflow:      "reminder",
		Subject:       "Reminder",
		Body:          in.Seed.Body,
		Channels:      channels,
		Recipients:    recipients,
	}, nil
}

// natsPort is the port the engine binds and the broadcaster dials. It reads
// the config value so DAGNATS_NATS_PORT is honored on both sides; the constant
// is the fallback when a zero value slipped through (tests, hand-built cfg).
func natsPort(cfg *config.Config) int {
	if cfg.DagNats.NATSPort > 0 {
		return cfg.DagNats.NATSPort
	}
	return dagnatsNATSPort
}

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
func createOnboardingTodo(ctx context.Context, todoH onboardingTodoCreator, step worker.TaskContext) error {
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
		slog.Error("dagnats: create todo failed", "error", err)
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
	// dagnatsNATSPort is the default NATS port the engine owns. It is only a
	// fallback: DAGNATS_NATS_PORT is what config parses, and the engine and
	// the broadcaster must agree — using the constant on both sides made the
	// env var silently inert (the engine bound 4222 while the broadcaster
	// dialed the configured port).
	dagnatsNATSPort = 4222
	dagnatsMaxStore = 10 << 30 // 10 GiB JetStream store cap (required by dagnats)
)

// startDagNats boots the DagNats durable-workflow engine in the same
// binary on its own HTTP port (cfg.DagNats.HTTPAddr, default :8090). It
// registers the onboarding worker handlers (which write example todos to
// the main todos table) and starts the engine.
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
func startDagNats(ctx context.Context, cfg *config.Config, todoH onboardingTodoCreator, notifier reminderService) {
	lifecycleCtx := ctx
	if !cfg.DagNats.Enabled {
		return
	}

	srv := appdagnats.NewServer(cfg.DagNats.StoreDir, cfg.DagNats.HTTPAddr,
		natsPort(cfg), dagnatsMaxStore,
	)
	dagNatsServer = srv
	// The feature gets the workflow client here, where the engine lives, so
	// features/notifications never imports internal/dagnats. A trimmed engine
	// therefore leaves the inbox working and the reminder form hidden.
	notifier.SetRunStarter(appdagnats.NewClient("http://" + cfg.DagNats.HTTPAddr))

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
		slog.Info("dagnats: onboarding greet")
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
		slog.Info("dagnats: awaiting first todo signal", "run", ctx.RunID())
		const signalTimeout = 50 * time.Minute
		_, err := ctx.WaitForSignal("first-todo", signalTimeout)
		if err != nil {
			slog.Warn("dagnats: await first-todo timed out/failed", "error", err)
			return ctx.Fail(err)
		}
		slog.Info("dagnats: first todo signal received", "run", ctx.RunID())
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
		slog.Info("dagnats: onboarding finalized")
		// The same delivery path the reminder uses: onboarding ends with one
		// inbox message, so the template has one answer for "tell the user",
		// not one per workflow. The transaction id makes a retried finalize a
		// no-op instead of a second welcome.
		var in struct {
			User string `json:"user"`
		}
		if err := json.Unmarshal(ctx.Input(), &in); err != nil {
			return ctx.Fail(err)
		}
		if in.User != "" {
			stepCtx := ctxForStep(lifecycleCtx, ctx)
			if derr := notifier.Deliver(stepCtx, notificationhandlers.Delivery{
				Owner:         in.User,
				TransactionID: "onboarding-welcome:" + in.User,
				Workflow:      "onboarding",
				Subject:       "Onboarding complete",
				Body:          "Your example todos are in the list. This message proves the shared delivery path.",
			}); derr != nil {
				slog.Error("dagnats: onboarding welcome failed", "error", derr)
				return ctx.Fail(derr)
			}
		}
		return ctx.Complete(ctx.Input())
	})
	// notification-deliver is the last step of the reminder workflow: the
	// sleep step before it holds the delay, so this runs at the moment the
	// user asked for. The payload rides the `seed` dependency (a sleep step
	// has no output), and a duplicate delivery is a no-op (the row is deduped
	// by owner + transaction + channel).
	shim.Handle("reminder-seed", func(ctx worker.TaskContext) error {
		// Root step: forward the run input so the deliver step can read it
		// after the wait. Completing with an empty payload would make the
		// delay lose the reminder.
		return ctx.Complete(ctx.Input())
	})
	shim.Handle("notification-deliver", func(ctx worker.TaskContext) error {
		d, derr := reminderDelivery(ctx.Input())
		if derr != nil {
			return ctx.Fail(derr)
		}
		if derr := notifier.Deliver(ctxForStep(lifecycleCtx, ctx), d); derr != nil {
			slog.Error("dagnats: deliver reminder failed", "error", derr)
			return ctx.Fail(derr)
		}
		return ctx.Complete(ctx.Input())
	})

	// Register every workflow definition this binary owns, idempotently, so
	// they stay in sync with the code. The REST API only comes up once
	// srv.Run() binds the port, so do it in a retry loop that waits for the
	// API to be reachable.
	go registerWorkflowsWithRetry(ctx, cfg.DagNats.HTTPAddr)

	go func() {
		if err := srv.Run(); err != nil {
			slog.Warn("dagnats server stopped", "error", err)
		}
	}()
	slog.Info("dagnats: listening", "addr", cfg.DagNats.HTTPAddr)
}

// workflowDef is one definition this binary registers at boot. Names are for
// the log line only; the JSON is the contract.
type workflowDef struct {
	name string
	json string
}

// workflows returns the definitions this binary owns. Adding a durable flow
// means adding a row here and a worker above — nothing else.
func workflows() []workflowDef {
	return []workflowDef{
		{name: "onboarding", json: appdagnats.OnboardingWorkflowJSON},
		{name: "reminder", json: appdagnats.ReminderWorkflowJSON},
	}
}

// registerWorkflowsWithRetry registers every workflow, retrying until the
// DagNats REST API is reachable (it boots after srv.Run binds the port).
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
func registerWorkflowsWithRetry(ctx context.Context, httpAddr string) {
	const (
		attempts   = 30
		retryDelay = 500 * time.Millisecond
	)
	client := appdagnats.NewClient("http://" + httpAddr)
	for range attempts {
		if err := registerWorkflows(ctx, client); err != nil {
			if ctx.Err() != nil {
				return // shutting down — not a registration failure
			}
			if waitStep(ctx, retryDelay) != nil {
				return // cancelled during the backoff
			}
			continue
		}
		return
	}
	slog.Warn("dagnats workflow register failed after retries")
}

// registerWorkflows posts every definition and names the one that failed.
// A partial success is retried as a whole: re-registering an already-registered
// definition is idempotent upstream.
func registerWorkflows(ctx context.Context, client *appdagnats.Client) error {
	for _, def := range workflows() {
		if err := client.RegisterWorkflow(ctx, []byte(def.json)); err != nil {
			return err
		}
		slog.Info("dagnats: workflow registered", "name", def.name)
	}
	return nil
}

// shutdownDagNats stops the DagNats engine if this process started one.
//
// It used to only nil the pointer, with a comment claiming "the engine's
// Shutdown is wired internally — closing the process triggers graceful drain
// via the server's own signal handling". That was not true, and the way it
// failed is subtle: DagNats installs its OWN signal.Notify for
// SIGINT/SIGTERM (server.waitAndShutdown), exactly as a second
// signal.Notify owner would. On a SIGTERM the Go runtime delivers the signal to EVERY
// registered channel, so both handlers run concurrently, and DagNats' shutdown
// path completes (and the process exits) before our server's Start() returns and our
// deferred cleanups run — so `q.Close()` (queue workers, SQLite handle) was
// simply skipped. Observed with the real binary: the log ends at
// "• stopping orchestrator..." and "queue workers stopped" never appears.
//
// Calling Stop() here makes ours the deterministic owner: it closes the
// engine's stopCh, so its Run() performs the drain through the normal
// stopCh path instead of racing a signal handler. Stop is idempotent and
// safe to call after the engine already stopped.
func shutdownDagNats() {
	srv := dagNatsServer
	dagNatsServer = nil
	if srv != nil {
		srv.Stop()
	}
}
