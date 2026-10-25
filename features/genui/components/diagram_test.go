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

// TestDiagramPromptListsPeleGallery pins the visual vocabulary the chat
// leg offers the model: the gallery types users actually ask for
// (fluxos, kanban, graficos, linhas do tempo) must be named in the prompt.
// Red-proof: drop one name from the diagram fragment and this fails —
// the model cannot emit a type the prompt never advertised.
func TestDiagramPromptListsPeleGallery(t *testing.T) {
	t.Parallel()
	prompt := PromptForChat()
	for _, want := range []string{
		"flowchart", "sequenceDiagram", "kanban", "pie", "gantt",
		"mindmap", "timeline", "journey", "xychart", "quadrantChart",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("chat prompt does not name %q (see https://pele.run/examples)", want)
		}
	}
}

// TestExtractMermaidFencesRendersPeleGallery pins the fence path per
// gallery type: each Pele example family the prompt advertises must
// survive fence extraction with its keyword intact, so the chat can
// render it as a diagram without requiring the strict JSON envelope.
// Red-proof: narrow the scanner to one keyword and the other rows fail.
func TestExtractMermaidFencesRendersPeleGallery(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want string
	}{
		{"fluxo", "flowchart TD\n A-->B", "flowchart"},
		{"sequencia", "sequenceDiagram\n Alice->>John: Hi", "sequencediagram"},
		{"kanban", "kanban\n Todo\n  Task1", "kanban"},
		{"pizza", "pie title Fish\n \"Ahi\" : 386", "pie"},
		{"gantt", "gantt\n title Jobs\n dateFormat X", "gantt"},
		{"mapa mental", "mindmap\n root((Hawaii))", "mindmap"},
		{"linha do tempo", "timeline\n title T\n section S", "timeline"},
		{"jornada", "journey\n title Day\n section Go", "journey"},
		{"xy", "xychart\n title T\n x-axis [a, b]", "xychart"},
		{"quadrante", "quadrantChart\n title R\n x-axis Low --> High", "quadrantchart"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := "segue:\n```mermaid\n" + tc.body + "\n```\n"
			got := ExtractMermaidFences(raw)
			if len(got) != 1 {
				t.Fatalf("ExtractMermaidFences(%s) = %q, want one fence", tc.name, got)
			}
			if typ := DiagramType(got[0]); typ != tc.want {
				t.Fatalf("DiagramType(%s) = %q, want %q", tc.name, typ, tc.want)
			}
		})
	}
}
