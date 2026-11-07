package graph

import "testing"

func TestWorkflowsParsesOnboardingShape(t *testing.T) {
	raw := []byte(`{"name":"onboarding","steps":[
		{"id":"greet","task":"onboarding-greet","depends_on":[]},
		{"id":"await","task":"onboarding-await-first-todo","depends_on":["greet"]},
		{"id":"wait","type":"sleep","depends_on":["await"]}
	]}`)
	def, err := ParseWorkflow(raw)
	if err != nil {
		t.Fatalf("ParseWorkflow: %v", err)
	}
	doc := Workflows([]WorkflowDef{def})
	if doc.Source != "dagnats" {
		t.Fatalf("source = %q", doc.Source)
	}
	if len(doc.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3 (%v)", len(doc.Nodes), doc.Nodes)
	}
	// sleep step has no task: Meta falls back to type.
	for _, n := range doc.Nodes {
		if n.Label == "wait" && n.Meta != "sleep" {
			t.Fatalf("sleep meta = %q, want sleep", n.Meta)
		}
	}
	if len(doc.Edges) < 2 {
		t.Fatalf("edges = %v, want 2 depends edges", doc.Edges)
	}
}

func TestParseWorkflowRefusesMalformed(t *testing.T) {
	if _, err := ParseWorkflow([]byte("{oops")); err == nil {
		t.Fatal("malformed def = nil, want error")
	}
}
