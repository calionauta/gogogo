package graph

import (
	"strings"
	"testing"
)

func TestOverlayRunsPicksNewestPerWorkflow(t *testing.T) {
	defs := []WorkflowDef{{Name: "onboarding", Steps: []Step{
		{ID: "greet", Task: "onboarding-greet"},
		{ID: "await-first-todo", Task: "onboarding-await-first-todo", Depends: []string{"greet"}},
	}}}
	runs := []Run{
		{
			ID: "old", Workflow: "onboarding", Status: "completed", CreatedAt: "2026-11-01T10:00:00Z",
			Steps: map[string]StepStatus{"greet": {Status: "completed"}},
		},
		{
			ID: "new", Workflow: "onboarding", Status: "running", CreatedAt: "2026-11-07T10:00:00Z",
			Steps: map[string]StepStatus{
				"greet":            {Status: "completed"},
				"await-first-todo": {Status: "running"},
			},
		},
	}
	doc := OverlayRuns(defs, runs)
	if doc.Source != "dagnats" {
		t.Fatalf("source = %q, want dagnats", doc.Source)
	}
	byLabel := map[string]Node{}
	for _, n := range doc.Nodes {
		byLabel[n.Label] = n
	}
	if got := byLabel["greet"].Status; got != "completed" {
		t.Fatalf("greet status = %q, want completed", got)
	}
	if got := byLabel["await-first-todo"].Status; got != "running" {
		t.Fatalf("await status = %q, want running", got)
	}
	// The run itself is surfaced as a node so operators see run id + status.
	if byLabel["run new"] == (Node{}) {
		t.Fatal("expected run node for the newest run")
	}
	if got := byLabel["run new"].Status; got != "running" {
		t.Fatalf("run node status = %q, want running", got)
	}
}

func TestOverlayRunsWithoutRunsLeavesStatusEmpty(t *testing.T) {
	defs := []WorkflowDef{{Name: "reminder", Steps: []Step{{ID: "seed", Task: "reminder-seed"}}}}
	doc := OverlayRuns(defs, nil)
	for _, n := range doc.Nodes {
		if n.Kind == "dagnats" && n.Status != "" {
			t.Fatalf("status = %q, want empty without runs", n.Status)
		}
	}
	if len(doc.Edges) != 0 {
		t.Fatalf("edges = %v, want none for a single step", doc.Edges)
	}
}

func TestOverlayRunsSurfacesStepError(t *testing.T) {
	defs := []WorkflowDef{{Name: "onboarding", Steps: []Step{{ID: "greet", Task: "onboarding-greet"}}}}
	runs := []Run{{
		ID: "r1", Workflow: "onboarding", Status: "failed", CreatedAt: "2026-11-07T10:00:00Z",
		Steps: map[string]StepStatus{"greet": {Status: "failed", Error: "boom"}},
	}}
	doc := OverlayRuns(defs, runs)
	for _, n := range doc.Nodes {
		if n.Label == "greet" {
			if n.Meta == "" || !contains(n.Meta, "boom") {
				t.Fatalf("meta = %q, want the step error", n.Meta)
			}
			return
		}
	}
	t.Fatal("greet node missing")
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
