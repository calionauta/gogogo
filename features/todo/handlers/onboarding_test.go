// SCOPE:feature — HTTP todo onboarding stream tests (wire side only).
//
// What is proven here needs no engine: the stepper signal shape, the
// re-list on the terminal event, and the anonymous skip. The engine side
// (route auth, failure fan-out, step math) is proven in
// features/onboarding, which owns it.
package handlers

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/dbschema"
)

func TestHandlersOnboardingStepperSignals(t *testing.T) {
	f := newHandlersFixture(t)
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

// TestHandlersOnboardingCompletedRelists proves the terminal event re-lists:
// the example todos are created server-side by the worker, so without the
// re-list they would not appear until a reload.
func TestHandlersOnboardingCompletedRelists(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("onb-done@example.com")

	// A row that exists before the completion event, so the re-list has
	// something to render.
	owner := f.ownerOf(t, cookie)
	if _, err := f.q.CreateTodo(context.Background(), dbgen.CreateTodoParams{
		ID: dbschema.NewID(), Owner: owner, Title: "my first todo",
		Created: dbschema.Now(), Updated: dbschema.Now(),
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

// TestHandlersOnboardingAnonymousCompletedSkipsList proves an anonymous stream
// still gets the terminal signal (the stepper must release for the public
// demo too) without attempting an unscoped list.
func TestHandlersOnboardingAnonymousCompletedSkipsList(t *testing.T) {
	f := newHandlersFixture(t)
	rec := httptest.NewRecorder()
	sse := sseGen(t, rec)
	if err := f.h.applyWorkflowCompleted(context.Background(), sse, "", false); err != nil {
		t.Fatalf("anonymous applyWorkflowCompleted: %v", err)
	}
	if !strings.Contains(rec.Body.String(), `"workflowCompleted":true`) {
		t.Fatalf("anonymous completion missing workflowCompleted (body: %s)", rec.Body.String())
	}
}

// TestHandlersOnboardingCreateForWorker proves the DagNats worker's entry point
// writes a scoped row: the example todos are created programmatically, so
// without the owner they would never surface in the user's list.
func TestHandlersOnboardingCreateForWorker(t *testing.T) {
	f := newHandlersFixture(t)
	cookie := f.signup("onb-worker@example.com")
	owner := f.ownerOf(t, cookie)

	if err := f.h.CreateTodoForOnboarding(context.Background(), "worker task", owner); err != nil {
		t.Fatalf("CreateTodoForOnboarding: %v", err)
	}
	list, err := f.q.ListTodosByOwner(context.Background(), owner)
	if err != nil || len(list) != 1 {
		t.Fatalf("after worker create: %d rows, %v; want 1", len(list), err)
	}
	if list[0].Title != "worker task" {
		t.Fatalf("worker create wrote %q, want %q", list[0].Title, "worker task")
	}
}

// TestHandlersOnboardingCreateRejectsEmptyOwner pins the fail-fast contract the
// store's owner scoping depends on.
func TestHandlersOnboardingCreateRejectsEmptyOwner(t *testing.T) {
	f := newHandlersFixture(t)
	if err := f.h.CreateTodoForOnboarding(context.Background(), "orphan", ""); err == nil {
		t.Fatal("CreateTodoForOnboarding with empty owner must fail, got nil")
	}
	rows, qerr := f.q.ListTodosByOwner(context.Background(), "")
	if qerr == nil && len(rows) != 0 {
		t.Fatalf("no rows may be written for an empty owner, found %d", len(rows))
	}
}
