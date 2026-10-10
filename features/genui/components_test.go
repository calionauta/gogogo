// SCOPE:layer=feature,removal=feature — gogen-ui render tests.
package genui_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/features/genui"
)

// TestGenuiResultStates pins the three render states the critique demands:
// empty (guidance, not blank space), error (message + recovery), and full
// (every catalog component present with its heading hierarchy).
func TestGenuiResultStates(t *testing.T) {
	t.Parallel()
	render := func() string {
		t.Helper()
		var buf bytes.Buffer
		if err := genui.GenuiResult(nil, "", false).Render(context.Background(), &buf); err != nil {
			t.Fatalf("GenuiResult(empty): %v", err)
		}
		return buf.String()
	}
	empty := render()
	for _, want := range []string{`id="genui-result"`, "Ask anything", `aria-live="polite"`} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty result missing %q", want)
		}
	}

	var errBuf bytes.Buffer
	if err := genui.GenuiResult(nil, "model exploded", true).Render(context.Background(), &errBuf); err != nil {
		t.Fatalf("GenuiResult(error): %v", err)
	}
	for _, want := range []string{"model exploded", "Try asking again", "role=\"alert\""} {
		if !strings.Contains(errBuf.String(), want) {
			t.Errorf("error result missing %q", want)
		}
	}
}

// TestGenuiIndexWiring pins the page-level contracts: the Ask form
// posts as form-encoded (so handleAsk reads prompt via FormValue), owns
// a network-failure release (datastar-fetch error stages reset the
// server-owned spinner — red-proofed below), labels its input, and opens
// the result stream exactly once.
func TestGenuiIndexWiring(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := genui.GenuiIndex("a@b.c", "dev", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("GenuiIndex: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		`id="genui-ask-form"`,
		`for="genui-prompt"`,
		`name="prompt"`,
		`contentType: &#39;form&#39;`,
		`data-on:datastar-fetch="evt.detail`,
		"retries-failed",
		`id="genui-stream-opener"`,
		`id="genui-result"`,
		`<h1`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index missing %q", want)
		}
	}
}

// its content (headings + data), so the worker's HTML assertions hold.
func TestGenuiResultRendersCatalog(t *testing.T) {
	t.Parallel()
	dirs := []genui.Directive{
		{Component: "text_note", Props: map[string]any{"text": "hello"}},
		{Component: "plan_cards", Props: map[string]any{"plans": []any{
			map[string]any{"title": "Ship it", "detail": "today"},
		}}},
		{Component: "data_table", Props: map[string]any{
			"headers": []any{"A", "B"},
			"rows":    []any{[]any{"1", "2"}},
		}},
	}
	comps := make([]templ.Component, 0, len(dirs))
	for _, d := range dirs {
		comp, err := genui.Render(d)
		if err != nil {
			t.Fatalf("Render(%q): %v", d.Component, err)
		}
		comps = append(comps, comp)
	}
	var buf bytes.Buffer
	if err := genui.GenuiResult(comps, "", false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("GenuiResult(full): %v", err)
	}
	for _, want := range []string{"hello", "Ship it", "today", "A", "B", "1", "<table", "<h3", "Apply"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("full result missing %q", want)
		}
	}
}
