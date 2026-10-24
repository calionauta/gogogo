// SCOPE:plugin — Mermaid diagram source validation + fence extraction.
package components

import (
	"errors"
	"strings"
)

// maxDiagramBytes bounds one diagram source: model markup past the cap is
// rejected at parse time (same shape as html_block), and it bounds the
// client render work itself.
const maxDiagramBytes = 8000

// CleanDiagramSource normalizes raw Mermaid syntax from a directive prop.
// It strips a surrounding ```mermaid fence when the model included one
// inside the prop, trims space, and enforces non-empty + size caps.
// The source stays plain text: Templ escapes it, Pele renders the SVG.
func CleanDiagramSource(raw string) (string, error) {
	src := strings.TrimSpace(raw)
	if strings.HasPrefix(src, "```") {
		if i := strings.Index(src, "\n"); i >= 0 {
			src = src[i+1:]
		}
		if i := strings.LastIndex(src, "```"); i >= 0 {
			src = src[:i]
		}
		src = strings.TrimSpace(src)
	}
	if src == "" {
		return "", errors.New("genui: diagram requires source")
	}
	if len(src) > maxDiagramBytes {
		return "", errors.New("genui: diagram exceeds 8000 bytes")
	}
	return src, nil
}

// DiagramType returns the leading Mermaid type keyword (flowchart,
// sequenceDiagram, classDiagram, ...) lowercased, or "" when empty.
// Best-effort for logs and repair feedback; the client renders whatever
// Pele supports, the server never allowlists types.
func DiagramType(src string) string {
	fields := strings.Fields(strings.TrimSpace(src))
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

// ExtractMermaidFences returns every ```mermaid block in raw prose, in
// order, cleaned with CleanDiagramSource. Non-mermaid fences are ignored,
// so ```json envelopes never become diagrams.
func ExtractMermaidFences(raw string) []string {
	var out []string
	rest := raw
	for {
		open := strings.Index(rest, "```")
		if open < 0 {
			break
		}
		after := rest[open+3:]
		nl := strings.Index(after, "\n")
		if nl < 0 {
			break
		}
		lang := strings.TrimSpace(strings.ToLower(after[:nl]))
		after = after[nl+1:]
		closed := strings.Index(after, "```")
		if closed < 0 {
			break
		}
		body := after[:closed]
		rest = after[closed+3:]
		if lang != "mermaid" {
			continue
		}
		if src, err := CleanDiagramSource(body); err == nil {
			out = append(out, src)
		}
	}
	return out
}
