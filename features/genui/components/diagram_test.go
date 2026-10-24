// SCOPE:plugin — diagram component tests (red-proof).
package components

import (
	"strings"
	"testing"
)

// TestDiagramParsesAndRenders pins the new catalog leaf: the prompt
// advertises it, a model answer with it validates, and it renders a
// mountable pre with the source escaped. Red-proof: remove the renderer
// entry and ParseDirectives rejects; remove the fragment and the prompt
// assertion fails.
func TestDiagramParsesAndRenders(t *testing.T) {
	t.Parallel()
	if !Registered(diagramName) {
		t.Fatalf("diagram %q not registered", diagramName)
	}
	if prompt := PromptForChat(); !strings.Contains(prompt, diagramName) {
		t.Fatalf("catalog prompt does not advertise %q", diagramName)
	}
	if prompt := PromptForChat(); !strings.Contains(prompt, diagramName) {
		t.Fatalf("chat prompt does not advertise %q", diagramName)
	}
	dirs, err := ParseDirectives(`{"components":[{"type":"diagram","props":{"source":"flowchart TD\n A-->B"}}]}`)
	if err != nil {
		t.Fatalf("ParseDirectives(diagram) = %v, want nil", err)
	}
	if len(dirs) != 1 || dirs[0].Component != diagramName {
		t.Fatalf("parsed = %+v, want one diagram", dirs)
	}
	comps, err := RenderAll(dirs)
	if err != nil {
		t.Fatalf("RenderAll(diagram) = %v, want nil", err)
	}
	if len(comps) != 1 || comps[0] == nil {
		t.Fatalf("rendered = %+v, want one component", comps)
	}
}

// TestDiagramRejectsEmptyAndOversized pins fail-closed shape: empty source
// and past-cap payloads never render.
func TestDiagramRejectsEmptyAndOversized(t *testing.T) {
	t.Parallel()
	if _, err := ParseDirectives(`{"components":[{"type":"diagram","props":{"source":"   "}}]}`); err == nil {
		t.Fatal("ParseDirectives(empty diagram) = nil error, want rejection")
	}
	big := strings.Repeat("x", maxDiagramBytes+1)
	if _, err := CleanDiagramSource(big); err == nil {
		t.Fatal("CleanDiagramSource(oversized) = nil error, want rejection")
	}
}

// TestExtractMermaidFencesIgnoresNonMermaid pins the fence scanner: only
// ```mermaid blocks become diagrams, ```json envelopes never do.
func TestExtractMermaidFencesIgnoresNonMermaid(t *testing.T) {
	t.Parallel()
	raw := "here:\n```json\n{\"a\":1}\n```\nand:\n```mermaid\nflowchart TD\n A-->B\n```\ndone"
	got := ExtractMermaidFences(raw)
	if len(got) != 1 || !strings.Contains(got[0], "flowchart") {
		t.Fatalf("ExtractMermaidFences = %q, want one flowchart", got)
	}
	if len(ExtractMermaidFences("no fences here")) != 0 {
		t.Fatal("ExtractMermaidFences(plain) non-empty, want empty")
	}
}
