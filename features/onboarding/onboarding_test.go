// SCOPE:feature — onboarding engine contract tests.
//
// What is proven here needs no todo slice: the route is owner-scoped, an
// unreachable engine surfaces as a workflow-error event rather than a
// spinning stepper, and the step math maps engine statuses to the stepper.
// The stream side (signal shape, re-list on completion) is proven in
// features/todo/handlers, which owns that wire.
package onboarding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/db"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
	"github.com/calionauta/gogogo/internal/queue"
)

type fixture struct {
	t   testing.TB
	r   http.Handler
	hub *queue.SSEHub
}

func newFixture(tb testing.TB) *fixture {
	tb.Helper()
	sqlDB, err := db.OpenAt(tb.TempDir() + "/onboarding.db")
	if err != nil {
		tb.Fatalf("db open: %v", err)
	}
	tb.Cleanup(func() { sqlDB.Close() })
	if merr := dbschema.Migrate(sqlDB); merr != nil {
		tb.Fatalf("db migrate: %v", merr)
	}
	a, err := auth.New(dbgen.New(sqlDB), false)
	if err != nil {
		tb.Fatalf("auth new: %v", err)
	}
	hub := queue.NewSSEHub()
	// 127.0.0.1:1 is closed: a StartRun here fails fast instead of hanging,
	// which is what the error-path test exercises.
	onb := NewHandler(a, "http://127.0.0.1:1", hub)

	r := http.NewServeMux()
	r.Handle("/api/auth/", a.Routes())
	r.Handle("/api/onboarding", onb.Routes())
	r.Handle("/api/onboarding/", onb.Routes())
	return &fixture{t: tb, r: r, hub: hub}
}

func (f *fixture) signup(email string) *http.Cookie {
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

func TestOnboardingRequiresAuth(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/onboarding/start", nil)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anon onboarding start: status = %d, want 303", rec.Code)
	}
}

// TestOnboardingEngineDownPublishesError proves the start route answers the
// client and then reports an unreachable engine as a workflow-error event
// rather than leaving the stepper spinning. The engine is never reached
// synchronously, so the route itself must be 200.
func TestOnboardingEngineDownPublishesError(t *testing.T) {
	f := newFixture(t)
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

func TestOnboardingStepMath(t *testing.T) {
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
			wantPhase: phaseGreet,
		},
		{
			name:      "awaiting first todo",
			steps:     map[string]any{"await-first-todo": map[string]any{"status": "running"}},
			wantStep:  2,
			wantPhase: phaseWorkflow,
		},
		{
			name:      "creating example todos",
			steps:     map[string]any{"todo-2": map[string]any{"status": "running"}},
			wantStep:  4,
			wantPhase: phaseWorkflow,
		},
		{
			name:      "failure surfaces as error phase",
			steps:     map[string]any{"todo-1": map[string]any{"status": "failed", "detail": "boom"}},
			wantStep:  3,
			wantPhase: phaseError,
		},
		{
			name:      "nothing running stays on greet",
			steps:     map[string]any{},
			wantStep:  1,
			wantPhase: phaseGreet,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step, phase, _ := currentStep(tc.steps)
			if step != tc.wantStep || phase != tc.wantPhase {
				t.Fatalf("currentStep = (%d, %s), want (%d, %s)",
					step, phase, tc.wantStep, tc.wantPhase)
			}
		})
	}
}
