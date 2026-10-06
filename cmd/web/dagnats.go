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
func startDagNats(cfg *config.Config, _ *pocketbase.PocketBase, todoH *handlers.TodoHandler) {
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
		const greetDelay = 1500 * time.Millisecond
		time.Sleep(greetDelay) // visible pace
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
	shim.Handle("onboarding-create-todo", func(ctx worker.TaskContext) error {
		// The run input is {"user": ..., "todos": [...]}; greet (root)
		// forwards it, and every downstream step's Input is its single
		// dependency's output, so each create-todo step receives the same
		// shape with whatever todos remain. Titles ride this input/output
		// chain rather than step config because the engine's live publish
		// path still omits TaskPayload.Config (per-step `metadata` IS
		// delivered as of DagNats v0.0.22, but config never was).
		var input struct {
			User  string   `json:"user"`
			Todos []string `json:"todos"`
		}
		if len(ctx.Input()) > 0 {
			_ = json.Unmarshal(ctx.Input(), &input)
		}
		text := ""
		if len(input.Todos) > 0 {
			text, input.Todos = input.Todos[0], input.Todos[1:]
		}
		if text == "" {
			text = "Onboarding task"
		}
		// Scoping the example todos to the owner that started the run is
		// what makes them visible in the user's list (the list query
		// filters by owner). Previously owner was hardcoded to "" so the
		// todos were created owner-less and never surfaced.
		if err := todoH.CreateTodoForOnboarding(text, input.User); err != nil {
			log.Printf("dagnats: create todo failed: %v", err)
			return ctx.Fail(err)
		}
		// Thread the remaining titles (and owner) forward so the next
		// create-todo step picks up the next example todo.
		out, err := json.Marshal(input)
		if err != nil {
			return ctx.Fail(err)
		}
		return ctx.Complete(out)
	})
	shim.Handle("onboarding-finalize", func(ctx worker.TaskContext) error {
		log.Printf("dagnats: onboarding finalized")
		return ctx.Complete(ctx.Input())
	})

	// Register the onboarding workflow definition idempotently so it is
	// always in sync with this binary. The REST API only comes up once
	// srv.Run() binds the port, so do it in a retry loop that waits for
	// the API to be reachable.
	go registerOnboardingWorkflowWithRetry(cfg.DagNats.HTTPAddr)

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
func registerOnboardingWorkflowWithRetry(httpAddr string) {
	client := appdagnats.NewClient("http://" + httpAddr)
	for range 30 {
		if err := client.RegisterWorkflow(context.Background(), []byte(appdagnats.OnboardingWorkflowJSON)); err != nil {
			time.Sleep(500 * time.Millisecond)
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
