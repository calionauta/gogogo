package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/danmestas/dagnats/server"
	"github.com/danmestas/dagnats/worker"

	"github.com/calionauta/gogogo/internal/dagnats"
	"github.com/calionauta/gogogo/internal/nats"
	"github.com/calionauta/gogogo/internal/queue"
)

// TestOnboarding_ResumeSignalsRun is the end-to-end regression guard
// for the app→workflow signal path. It boots a real DagNats
// server, registers the onboarding workflow, starts a run, then
// drives the exact code the create-todo path triggers:
// OnboardingHandler.ResumeOnboarding(user) → client.Signal(first-todo)
// → the blocked WaitForSignal step resumes → run completes.
//
// If someone breaks the signal name ("first-todo"), the
// runID plumbing (activeRunID), or the ResumeOnboarding wiring,
// this test fails instead of hanging silently in production.
//
// The engine binds an EPHEMERAL HTTP port (`127.0.0.1:0`) and the test reads
// the real address back from srv.HTTPAddr(). A fixed port here collided with
// internal/nats's test, which used the same 18099 — that clash (not CPU, not
// an engine limitation) is why these packages had to run under `-p 1`. NATS
// itself already used -1 (random), so only the HTTP side needed fixing.
func TestOnboarding_ResumeSignalsRun(t *testing.T) {
	hub := queue.NewSSEHub()
	broadcaster := nats.NewInMemoryBroadcaster(hub)

	// Boot a real DagNats server; NATS on an ephemeral port too (-1).
	srv := dagnats.NewServer(t.TempDir(), "127.0.0.1:0", -1, 1<<30)

	h := &OnboardingHandler{
		broadcaster: broadcaster,
	}

	// Register the same task handlers the real app registers in
	// cmd/web/dagnats.go (names must match OnboardingWorkflowJSON).
	// EmbeddedWorker MUST be called before Run().
	shim := server.EmbeddedWorker(srv)
	shim.Handle("onboarding-greet", func(ctx worker.TaskContext) error {
		return ctx.Complete([]byte(`"welcomed"`))
	})
	shim.Handle("onboarding-await-first-todo", func(ctx worker.TaskContext) error {
		if _, err := ctx.WaitForSignal("first-todo", 50*time.Minute); err != nil {
			return ctx.Fail(err)
		}
		return ctx.Complete([]byte(`"resumed"`))
	})
	shim.Handle("onboarding-create-todo", func(ctx worker.TaskContext) error {
		return ctx.Complete([]byte(`"created"`))
	})
	shim.Handle("onboarding-finalize", func(ctx worker.TaskContext) error {
		return ctx.Complete([]byte(`"done"`))
	})

	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run() }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-runErr; err != nil {
			t.Logf("dagnats test server stopped: %v", err)
		}
	})

	// Wait until the engine reports its bound address, then point the client
	// at it. HTTPAddr() is empty until ready, so this doubles as readiness.
	httpAddr := waitForEphemeralAddr(t, srv)
	h.client = dagnats.NewClient("http://" + httpAddr)
	waitForDagNatsReady(t, httpAddr)

	ctx := context.Background()
	registerOnboardingWorkflow(t, h.client)

	runID, err := h.client.StartRun(ctx, "onboarding", map[string]any{"user": "tester"})
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	if runID == "" {
		t.Fatal("empty run id")
	}

	// The create path sets h.activeRunID (via handleStart) and then
	// calls ResumeOnboarding on first todo. We mirror that here.
	h.mu.Lock()
	h.activeRunID = runID
	h.mu.Unlock()

	// Simulate the user creating their first todo → resume the run.
	h.ResumeOnboarding("tester")

	// Run must now complete (greet → await-signaled → create x3 → finalize).
	// Under -race the engine is slower, so allow up to 30s.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, err := h.client.GetRun(ctx, runID)
		if err == nil && st.Status == "completed" {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("onboarding run did not complete after ResumeOnboarding signal")
}

// waitForEphemeralAddr blocks until the engine reports the address it actually
// bound (HTTPAddr() is empty until the server is ready) and returns it. This is
// what lets the test use `127.0.0.1:0` instead of a fixed port that could
// collide with a test in another package.
func waitForEphemeralAddr(t *testing.T, srv *server.Server) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if addr := srv.HTTPAddr(); addr != "" {
			return addr
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("dagnats never reported a bound HTTP address")
	return ""
}

func waitForDagNatsReady(t *testing.T, httpAddr string) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		req, reqErr := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+httpAddr+"/ready", nil)
		if reqErr != nil {
			t.Fatalf("ready request: %v", reqErr)
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("dagnats /ready not reached within timeout")
}

func registerOnboardingWorkflow(t *testing.T, client *dagnats.Client) {
	t.Helper()
	for range 80 {
		if err := client.RegisterWorkflow(context.Background(), []byte(dagnats.OnboardingWorkflowJSON)); err == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("failed to register onboarding workflow")
}
