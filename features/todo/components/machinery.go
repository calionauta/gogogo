// SCOPE:feature — The three machinery sequences as data.
//
// The queue+retry, AI-suggest and durable-workflow phase lists used to be three
// hand-written <ul class="steps"> blocks with per-step `data-class` objects
// inline. Six workflow steps meant six near-identical expressions, which had
// already drifted in formatting and could not render without `.steps`.
//
// They are data now: one []ui.Step per sequence, rendered by internal/ui.StepList.
// The expressions are the ones the page always used — verbatim, so the live
// phases behave exactly as before; only the markup and the state vocabulary
// moved to one place.
package components

import (
	"strconv"

	"github.com/calionauta/gogogo/internal/ui"
)

// queueSteps is the queue + retry demo: enqueue (goqite), retry (retry-go),
// complete. Hard-coded work, no LLM involved.
func queueSteps() []ui.Step {
	return []ui.Step{
		{Label: "Enqueue hard-coded job (goqite)", Done: "$demoStep >= 2", Active: "$demoStep === 1"},
		{
			Label:      "Retry on simulated failure (retry-go)",
			Done:       "$demoStep >= 3",
			Active:     "$demoStep === 2",
			Detail:     "'Retrying — attempt ' + ($lastRetryAttempt || 1) + ' (retry-go)'",
			DetailShow: "$lastRetryStatus === 'attempt'",
		},
		{
			Label:  "Job completes",
			Done:   "$demoStep >= 3 && $techPhase !== 'error'",
			Error:  "$techPhase === 'error'",
			Active: "$demoStep === 3 && $techPhase === 'error'",
		},
	}
}

// aiSteps is the AI-suggest sequence: queue the job, retry transient failures,
// return suggestions. Gated on the LLM layer by the caller.
func aiSteps() []ui.Step {
	const (
		settled   = "$aiStep >= 1 && !$aiPending && $aiPhase !== 'error'"
		aiErrored = "$aiPhase === 'error'"
	)
	return []ui.Step{
		{
			Label:  "Queue suggest job (goqite)",
			Done:   settled,
			Active: "$aiStep >= 1 && $aiPending",
			Error:  aiErrored,
		},
		{
			Label:      "Retry on transient error (retry-go)",
			Done:       settled,
			Active:     "$aiStep >= 1 && $aiPending",
			Error:      aiErrored,
			Detail:     "'Retrying — attempt ' + ($lastRetryAttempt || 1) + ' (retry-go)'",
			DetailShow: "$lastRetryStatus === 'attempt'",
		},
		{
			Label:  "Return suggestions",
			Done:   "$aiStep >= 3 && !$aiPending && $aiPhase !== 'error'",
			Active: "$aiStep >= 3 && $aiPending",
			Error:  aiErrored,
		},
	}
}

// workflowPhases is the durable onboarding pipeline, in order. The index is the
// workflow's own step number (1-based), so the phase a signal reports and the
// phase shown cannot disagree.
var workflowPhases = []string{
	"Greeting",
	"Waiting for your first to-do",
	"Creating example todo 1/3",
	"Creating example todo 2/3",
	"Creating example todo 3/3",
	"Finalize",
}

// workflowSteps renders one ui.Step per phase, with the two expressions the
// original markup repeated six times: active on the current step, done once the
// workflow passed it (or completed outright).
func workflowSteps() []ui.Step {
	steps := make([]ui.Step, 0, len(workflowPhases))
	for i, name := range workflowPhases {
		n := i + 1
		steps = append(steps, ui.Step{
			Label: strconv.Itoa(n) + ". " + name,
			Active: "$techStep === 'workflow' && $onboardingActive && !$workflowCompleted && " +
				"$onboardingStep === " + strconv.Itoa(n),
			Done: "$workflowCompleted || ($onboardingActive && !$workflowCompleted && " +
				"$onboardingStep > " + strconv.Itoa(n) + ")",
		})
	}
	return steps
}
