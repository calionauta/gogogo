// SCOPE:feature — chat visual-gallery + smalltalk red-proofs.
// Split from parse_test.go for the 500-line file gate.
package store

import (
	"strings"
	"testing"
)

// TestParseChatAnswerRendersPeleGallery pins the end-to-end chat path
// for the visual requests users actually make (fluxo, kanban, grafico):
// prose around a ```mermaid fence becomes prose + a diagram directive,
// never the unrenderable placeholder. Each row is one Pele gallery
// family from https://pele.run/examples. Red-proof: drop the fence
// scan in resolveChatAnswer and every row degrades to a prose bubble
// carrying raw fence text.
func TestParseChatAnswerRendersPeleGallery(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{"fluxo de rotina", "flowchart TD\n Receber-->Triar-->Responder"},
		{"kanban", "kanban\n Todo\n  Escrever minuta"},
		{"grafico pizza", "pie title Votos\n \"Sim\" : 60\n \"Nao\" : 40"},
		{"sequencia", "sequenceDiagram\n Cliente->>Loja: Pedido"},
		{"cronograma", "gantt\n title Entregas\n dateFormat YYYY-MM-DD"},
		{"mapa mental", "mindmap\n root((Escritorio))"},
		{"linha do tempo", "timeline\n title Casos\n section 2024"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := "Aqui esta o visual:\n```mermaid\n" + tc.body + "\n```\nEspero que ajude."
			dirs, dropped, _, err := ParseChatAnswer(raw)
			if err != nil {
				t.Fatalf("ParseChatAnswer(%s): %v", tc.name, err)
			}
			if dropped != 0 {
				t.Fatalf("ParseChatAnswer(%s) dropped %d, want 0", tc.name, dropped)
			}
			var sawDiagram, sawProse bool
			for _, d := range dirs {
				switch d.Component {
				case "diagram":
					sawDiagram = true
					src, _ := d.Props["source"].(string)
					if src == "" {
						t.Fatalf("%s: diagram source empty", tc.name)
					}
				case "text_note":
					sawProse = true
				}
			}
			if !sawDiagram {
				t.Fatalf("ParseChatAnswer(%s) = %+v, want a diagram directive", tc.name, dirs)
			}
			if !sawProse {
				t.Fatalf("ParseChatAnswer(%s) = %+v, want the prose kept beside the diagram", tc.name, dirs)
			}
			if _, err := ParseChatAnswerForRepair(raw); err != nil {
				t.Fatalf("gallery answer %s flagged for repair: %v", tc.name, err)
			}
		})
	}
}

// TestParseChatAnswerRendersDiagramEnvelope pins the strict path for a
// visual request: a well-formed diagram envelope validates with no
// fallback, so a fluxo/kanban answer never hits the unrenderable line.
func TestParseChatAnswerRendersDiagramEnvelope(t *testing.T) {
	t.Parallel()
	raw := `{"components":[{"type":"diagram","props":{"source":"kanban\n Todo\n  Escrever minuta"}}]}`
	dirs, dropped, fallback, err := ParseChatAnswer(raw)
	if err != nil {
		t.Fatalf("ParseChatAnswer(diagram envelope): %v", err)
	}
	if dropped != 0 || fallback {
		t.Fatalf("diagram envelope dropped=%d fallback=%v, want 0/false", dropped, fallback)
	}
	if len(dirs) != 1 || dirs[0].Component != "diagram" {
		t.Fatalf("parsed = %+v, want one diagram", dirs)
	}
}

// TestParseChatAnswerKeepsSmalltalkAsProse pins the non-visual half:
// bate-papo (oi, obrigado, o que voce faz) must stay a single prose
// bubble — never a diagram, never a catalog component — and the repair
// gate must spare it, or every greeting would cost a model retry and
// come back worse. Red-proof: route prose into the repair path and
// the ForRepair assertions go red.
func TestParseChatAnswerKeepsSmalltalkAsProse(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"oi",
		"obrigado!",
		"tudo bem?",
		"o que voce pode fazer?",
		"quem e voce?",
		"boa tarde, me conta uma piada",
	} {
		dirs, dropped, fallback, err := ParseChatAnswer(raw)
		if err != nil {
			t.Fatalf("ParseChatAnswer(%q): %v", raw, err)
		}
		if dropped != 0 {
			t.Fatalf("ParseChatAnswer(%q) dropped %d, want 0", raw, dropped)
		}
		if !fallback || len(dirs) != 1 || dirs[0].Component != "text_note" {
			t.Fatalf("ParseChatAnswer(%q) = %+v fallback=%v, want 1 text_note", raw, dirs, fallback)
		}
		for _, d := range dirs {
			if d.Component == "diagram" {
				t.Fatalf("ParseChatAnswer(%q) rendered a diagram for smalltalk", raw)
			}
		}
		text, _ := dirs[0].Props["text"].(string)
		if !strings.Contains(text, strings.TrimSpace(raw)) {
			t.Fatalf("prose bubble %q lost the greeting %q", text, raw)
		}
		if _, err := ParseChatAnswerForRepair(raw); err != nil {
			t.Fatalf("smalltalk %q flagged for repair: %v", raw, err)
		}
	}
}
