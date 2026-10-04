// SCOPE:layer=feature,removal=feature — Onboarding SSE progress vocabulary.
//
// Pure step math + phase/status constants shared by the DagNats onboarding
// handler (onboarding.go) and the todo SSE dispatcher (todo_sse.go). This
// file has NO DagNats import on purpose: when the installer trims the
// `dagnats` unit it deletes onboarding.go but KEEPS this file, so the
// stepper UI keeps compiling with the workflow reduced to its completed/
// error terminal states.
package handlers

import (
	"fmt"
	"time"
)

// Onboarding phase strings extracted as constants to avoid repeating
// string literals across pollRun, onboardingCurrentStep, and
// onboardingFailedStep (goconst).
const (
	onbPhaseGreet        = "greet"
	onbPhaseWorkflow     = "workflow"
	onbPhaseFinalize     = "finalize"
	onbPhaseError        = "error"
	onbStatusCompleted   = "completed"
	onbStatusFailed      = "failed"
	onbDetailGreet       = "Greeting user"
	onbDetailWaitForTodo = "Waiting for your next to-do (create one to continue)"
	onbDetailFinalize    = "Finalizing onboarding"

	// onbSignalTimeout is the context deadline for signalling the
	// blocked WaitForSignal step in ResumeOnboarding.
	onbSignalTimeout = 5 * time.Second

	// onbPollTimeout is the hard ceiling for pollRun's polling loop.
	onbPollTimeout = 5 * time.Minute

	// onbPollInterval is the tick interval for pollRun.
	onbPollInterval = 700 * time.Millisecond
)

var onboardingStepOrder = []string{
	"greet", "await-first-todo", "todo-1", "todo-2", "todo-3", "finalize",
}

// onboardingCurrentStep returns the 1-based current step, the UI phase, and
// a human label derived from per-step statuses. A "running" step is the
// current step; if nothing is running yet we stay on step 1 (greet). A
// "failed" step surfaces as an error phase.
func onboardingCurrentStep(steps map[string]any) (int, string, string) {
	for i, id := range onboardingStepOrder {
		st, _ := steps[id].(map[string]any)
		status, _ := st["status"].(string)
		switch status {
		case "running":
			cur := i + 1
			//nolint:mnd // step numbers 1-6 are structural, not magic
			switch cur {
			case 1:
				return cur, onbPhaseGreet, onbDetailGreet
			case 2:
				return cur, onbPhaseWorkflow, onbDetailWaitForTodo
			case 3, 4, 5:
				return cur, onbPhaseWorkflow, fmt.Sprintf("Creating example todo %d/3", cur-2)
			default:
				return cur, onbPhaseFinalize, onbDetailFinalize
			}
		case onbStatusFailed:
			cur := i + 1
			detail := ""
			if d, ok := st["detail"].(string); ok {
				detail = d
			}
			return cur, onbPhaseError, detail
		}
	}
	return 1, "greet", "Greeting user"
}

// onboardingFailedStep returns the 1-based index and detail of the first
// failed step, defaulting to step 1 if none is found.
func onboardingFailedStep(steps map[string]any) (int, string) {
	for i, id := range onboardingStepOrder {
		st, _ := steps[id].(map[string]any)
		status, _ := st["status"].(string)
		if status == onbStatusFailed {
			detail := ""
			if d, ok := st["detail"].(string); ok {
				detail = d
			}
			return i + 1, detail
		}
	}
	return 1, "unknown error"
}
