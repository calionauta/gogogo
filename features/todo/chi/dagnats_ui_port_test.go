// SCOPE:feature — Ported red-proofs for the chi todo slice
//
// Three contracts the legacy handler suite pinned and the chi slice now
// re-pins: the Durable Workflow UI flag is registration truth (not
// config), fail() answers with the public message and never leaks the
// internal error, and the techstack stepper releases the spinner only on
// a terminal step. Ported from features/todo/handlers/{dagnats_ui,http_fail,
// spinner_reset}_test.go when the legacy handlers died.
package chi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/starfederation/datastar-go/datastar"
)

// stubResumer satisfies the resumer interface without an engine: the UI
// flag tests registration truth, not engine behaviour.
type stubResumer struct{}

func (stubResumer) ResumeOnboarding(string) {}

// TestChiDagNatsUISignalRequiresRegistration pins registration truth:
// the UI flag follows the wired Onboarding handler, never the config
// alone. bootOnboarding only registers when the engine is enabled, so
// the trimmed/disabled cases cannot show the tab.
func TestChiDagNatsUISignalRequiresRegistration(t *testing.T) {
	f := newChiFixture(t)
	if f.h.dagnatsUIEnabled() {
		t.Error("dagnatsUIEnabled = true with no Onboarding wired, want false")
	}
	f.h.SetOnboarding(stubResumer{})
	if !f.h.dagnatsUIEnabled() {
		t.Error("dagnatsUIEnabled = false with the handler registered, want true")
	}
}

// TestChiTodoFragmentCarriesDagNatsFlag is the end-to-end guard for the
// regression where the chi slice never set Signals.DagNatsEnabled: the
// Durable Workflow tab would never render. Registration flips the
// rendered page.
func TestChiTodoFragmentCarriesDagNatsFlag(t *testing.T) {
	f := newChiFixture(t)
	cookie := f.signup("workflow-tab@example.com")

	// The page builds from handleIndex' signals; the tab lives in the
	// sidebar component, so the page (not the list fragment) is where
	// the flag is observable.
	rec := f.do(http.MethodGet, "/todo", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("todo page status = %d", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, `$sidebarTab = 'workflow'`) {
		t.Fatal("Durable Workflow tab rendered with the engine unregistered")
	}

	f.h.SetOnboarding(stubResumer{})
	rec = f.do(http.MethodGet, "/todo", "", cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("todo page (registered) status = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `$sidebarTab = 'workflow'`) {
		t.Fatal("Durable Workflow tab missing with the engine registered (registration truth broken)")
	}
}

// TestChiFailWritesPublicMessageWithStatus pins the fail() contract:
// 500 with the public message, internal error logged only.
func TestChiFailWritesPublicMessageWithStatus(t *testing.T) {
	f := newChiFixture(t)
	rec := httptest.NewRecorder()
	f.h.fail(context.Background(), rec, "todo: save failed", errors.New("sqlite: busy"), "save failed")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "save failed") {
		t.Fatalf("body must contain the public message; got %q", body)
	}
	if strings.Contains(body, "sqlite: busy") {
		t.Fatalf("body must not leak the internal error; got %q", body)
	}
}

// TestChiApplyTechStepResetsSpinnerOnDone pins the terminal step: the
// spinner releases and techDone flips.
func TestChiApplyTechStepResetsSpinnerOnDone(t *testing.T) {
	f := newChiFixture(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/todos/retry-demo", nil)
	sse := sdk.NewSSE(rec, req)

	if err := f.h.applyTechStep(sse, "retry-demo", true, ""); err != nil {
		t.Fatalf("applyTechStep: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"suggestPending":false`) {
		t.Fatalf("done step must reset suggestPending=false; got:\n%s", body)
	}
	if !strings.Contains(body, `"techDone":true`) {
		t.Fatalf("done step must set techDone=true; got:\n%s", body)
	}
}

// TestChiApplyTechStepKeepsSpinnerWhileRunning pins the inverse: a
// non-terminal step keeps the spinner up.
func TestChiApplyTechStepKeepsSpinnerWhileRunning(t *testing.T) {
	f := newChiFixture(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/todos/retry-demo", nil)
	sse := sdk.NewSSE(rec, req)

	if err := f.h.applyTechStep(sse, "retry-demo", false, ""); err != nil {
		t.Fatalf("applyTechStep: %v", err)
	}
	body := rec.Body.String()
	if strings.Contains(body, `"suggestPending":false`) {
		t.Fatalf("running step must NOT release the spinner; got:\n%s", body)
	}
	if !strings.Contains(body, `"techStep":"retry-demo"`) {
		t.Fatalf("running step must carry techStep; got:\n%s", body)
	}
}
