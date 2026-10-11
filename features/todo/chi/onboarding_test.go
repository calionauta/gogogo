// SCOPE:feature — Chi onboarding contract tests (red-proof).
//
// The engine interaction (StartRun / Signal / GetRunRaw) is the DagNats
// client's contract and is covered by internal/dagnats. What is proven here
// is the chi stack's own wiring: the route is owner-scoped, the stepper
// signals merge with the right shape, and a finished run's
// "workflow-completed" event re-lists the example todos the worker created
// server-side. None of that needs a running engine, and a test that needed
// one would be skipped more often than it is read.
package chi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/starfederation/datastar-go/datastar"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/queue"
)

// onboardingFixture boots the stack with an onboarding handler aimed at a
// dead address: no route call reaches the engine in these tests, so the
// handler only supplies the signal merge and the event fan-out.
type onboardingFixture struct {
	t          testing.TB
	r          http.Handler
	h          *Handler
	onboarding *OnboardingHandler
	hub        *queue.SSEHub
	q          *dbgen.Queries
	auth       *auth.Auth
}

func newOnboardingFixture(tb testing.TB) *onboardingFixture {
	tb.Helper()
	sqlDB, err := db.OpenAt(tb.TempDir() + "/todo-chi-onb.db")
	if err != nil {
		tb.Fatalf("db open: %v", err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	if merr := db.Migrate(sqlDB); merr != nil {
		tb.Fatalf("db migrate: %v", merr)
	}
	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		tb.Fatalf("auth new: %v", err)
	}
	hub := queue.NewSSEHub()
	h := NewHandler(dbgen.New(sqlDB), a, hub, "daisyui")
	// 127.0.0.1:1 is closed: a StartRun here fails fast instead of hanging,
	// which is what the error-path test exercises.
	onb := NewOnboardingHandler(a, "http://127.0.0.1:1", hub)
	h.SetOnboarding(onb)

	r := http.NewServeMux()
	r.Handle("/api/auth/", a.Handler())
	r.Handle("/api/onboarding", onb.Routes())
	r.Handle("/api/onboarding/", onb.Routes())
	r.Handle("/", h.Routes())
	return &onboardingFixture{
		t: tb, r: r, h: h, onboarding: onb, hub: hub,
		q: dbgen.New(sqlDB), auth: a,
	}
}

func (f *onboardingFixture) signup(email string) *http.Cookie {
	f.t.Helper()
	body := `{"email":` + strconv.Quote(email) + `,"password":"Correct-horse-99!"}`
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/auth/signup/credential", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("signup %s: status = %d (body: %s)", email, rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	f.t.Fatalf("signup %s: no %q cookie", email, auth.CookieName)
	return nil
}

func TestChiOnboardingRequiresAuth(t *testing.T) {
	f := newOnboardingFixture(t)
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/onboarding/start", nil)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anon onboarding start: status = %d, want 303", rec.Code)
	}
}

// TestChiOnboardingEngineDownPublishesError proves the start route answers
// the client and then reports an unreachable engine as a workflow-error
// event rather than leaving the stepper spinning. The engine is never
// reached synchronously, so the route itself must be 200.
func TestChiOnboardingEngineDownPublishesError(t *testing.T) {
	f := newOnboardingFixture(t)
	cookie := f.signup("onb-down@example.com")

	// Register before the request: the hub drops messages with no matching
	// subscriber, and the failure event is published from a goroutine.
	ch := make(chan []byte, 16)
	f.hub.Register("onb-down-client", "", ch)
	defer f.hub.UnregisterIfCurrent("onb-down-client", ch)

	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/onboarding/start", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("onboarding start: status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("onboarding start: content-type = %q, want SSE", ct)
	}
	if !strings.Contains(rec.Body.String(), "onboardingStarted") {
		t.Fatalf("onboarding ack missing onboardingStarted (body: %s)", rec.Body.String())
	}

	// Wait for the fan-out instead of asserting a race with the goroutine.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case payload := <-ch:
			if strings.Contains(string(payload), "workflow-error") {
				return
			}
		case <-deadline:
			t.Fatal("workflow-error event never reached the hub after engine failure")
		case <-time.After(20 * time.Millisecond):
			// The event arrives on the goroutine's own schedule.
		}
	}
}

// sseGen builds an SSE generator over a recorder for the signal-merge
// tests, which have no live request to borrow a context from.
func sseGen(tb testing.TB, w http.ResponseWriter) *sdk.ServerSentEventGenerator {
	tb.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/todos", nil)
	return sdk.NewSSE(w, req)
}

func TestChiOnboardingStepperSignals(t *testing.T) {
	f := newOnboardingFixture(t)
	rec := httptest.NewRecorder()
	sse := sseGen(t, rec)
	if err := f.h.streamProgress(sse, []byte(
		`{"step":2,"total":6,"phase":"workflow","detail":"Waiting for your next to-do"}`,
	)); err != nil {
		t.Fatalf("streamProgress: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"onboardingActive":true`, `"onboardingStep":2`, `"onboardingTotal":6`,
		`"onboardingPhase":"workflow"`, `"techStep":"workflow"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stepper signals missing %s (body: %s)", want, body)
		}
	}
	if strings.Contains(body, `"workflowCompleted":true`) {
		t.Fatalf("mid-run progress must NOT mark workflowCompleted (body: %s)", body)
	}
}

// TestChiOnboardingCompletedRelists proves the terminal event re-lists:
// the example todos are created server-side by the worker, so without the
// re-list they would not appear until a reload.
func TestChiOnboardingCompletedRelists(t *testing.T) {
	f := newOnboardingFixture(t)
	cookie := f.signup("onb-done@example.com")

	// A row that exists before the completion event, so the re-list has
	// something to render.
	owner := f.ownerOf(t, cookie)
	if _, err := f.q.CreateTodo(context.Background(), dbgen.CreateTodoParams{
		ID: db.NewID(), Owner: owner, Title: "my first todo",
		Created: db.Now(), Updated: db.Now(),
	}); err != nil {
		t.Fatalf("seed todo: %v", err)
	}

	rec := httptest.NewRecorder()
	sse := sseGen(t, rec)
	if err := f.h.applyWorkflowCompleted(context.Background(), sse, owner, true); err != nil {
		t.Fatalf("applyWorkflowCompleted: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"workflowCompleted":true`) {
		t.Fatalf("completion missing workflowCompleted signal (body: %s)", body)
	}
	if !strings.Contains(body, "my first todo") {
		t.Fatalf("completion did not re-list the owner's todos (body: %s)", body)
	}
}

// TestChiOnboardingAnonymousCompletedSkipsList proves an anonymous stream
// still gets the terminal signal (the stepper must release for the public
// demo too) without attempting an unscoped list.
func TestChiOnboardingAnonymousCompletedSkipsList(t *testing.T) {
	f := newOnboardingFixture(t)
	rec := httptest.NewRecorder()
	sse := sseGen(t, rec)
	if err := f.h.applyWorkflowCompleted(context.Background(), sse, "", false); err != nil {
		t.Fatalf("anonymous applyWorkflowCompleted: %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"workflowCompleted":true`) {
		t.Fatalf("anonymous completion missing workflowCompleted (body: %s)", rec.Body.String())
	}
}

func TestChiOnboardingStepMath(t *testing.T) {
	tests := []struct {
		name      string
		steps     map[string]any
		wantStep  int
		wantPhase string
	}{
		{
			name:      "greeting",
			steps:     map[string]any{"greet": map[string]any{"status": "running"}},
			wantStep:  1,
			wantPhase: onbPhaseGreet,
		},
		{
			name:      "awaiting first todo",
			steps:     map[string]any{"await-first-todo": map[string]any{"status": "running"}},
			wantStep:  2,
			wantPhase: onbPhaseWorkflow,
		},
		{
			name:      "creating example todos",
			steps:     map[string]any{"todo-2": map[string]any{"status": "running"}},
			wantStep:  4,
			wantPhase: onbPhaseWorkflow,
		},
		{
			name:      "failure surfaces as error phase",
			steps:     map[string]any{"todo-1": map[string]any{"status": "failed", "detail": "boom"}},
			wantStep:  3,
			wantPhase: onbPhaseError,
		},
		{
			name:      "nothing running stays on greet",
			steps:     map[string]any{},
			wantStep:  1,
			wantPhase: onbPhaseGreet,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step, phase, _ := onboardingCurrentStep(tc.steps)
			if step != tc.wantStep || phase != tc.wantPhase {
				t.Fatalf("onboardingCurrentStep = (%d, %s), want (%d, %s)",
					step, phase, tc.wantStep, tc.wantPhase)
			}
		})
	}
}

// ownerOf resolves the test user's owner id through its session.
func (f *onboardingFixture) ownerOf(t *testing.T, cookie *http.Cookie) string {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	sess, err := f.auth.Session(req)
	if err != nil {
		t.Fatalf("ownerOf: %v", err)
	}
	return sess.User.ID
}
