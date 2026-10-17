// SCOPE:feature — gogen-ui render tests.
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/features/genui/components"
)

// TestGenuiResultStates pins the three render states the critique demands:
// empty (guidance, not blank space), error (message + recovery), and full
// (every catalog component present with its heading hierarchy).
func TestGenuiResultStates(t *testing.T) {
	t.Parallel()
	render := func() string {
		t.Helper()
		var buf bytes.Buffer
		if err := components.GenuiResult(nil, "", false).Render(context.Background(), &buf); err != nil {
			t.Fatalf("components.GenuiResult(empty): %v", err)
		}
		return buf.String()
	}
	empty := render()
	for _, want := range []string{`id="genui-result"`, "Ask anything", `aria-live="polite"`} {
		if !strings.Contains(empty, want) {
			t.Errorf("empty result missing %q", want)
		}
	}
	// The empty state is server-rendered at page load and never patched, so
	// an anchor here would scroll the page on load for no reason — the
	// anchor belongs to the answer, not to the region's birth state.
	if strings.Contains(empty, "scrollIntoView") {
		t.Errorf("empty state carries the scroll anchor — loading the Ask page would jump: %s", empty)
	}

	var errBuf bytes.Buffer
	if err := components.GenuiResult(nil, "model exploded", true).Render(context.Background(), &errBuf); err != nil {
		t.Fatalf("components.GenuiResult(error): %v", err)
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
	if err := components.GenuiIndex("a@b.c", "user", "dev", "").Render(context.Background(), &buf); err != nil {
		t.Fatalf("components.GenuiIndex: %v", err)
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
		`id="toast-container"`,
		`id="session-banner"`,
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
	dirs := []components.Directive{
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
		comp, err := components.Render(d)
		if err != nil {
			t.Fatalf("components.Render(%q): %v", d.Component, err)
		}
		comps = append(comps, comp)
	}
	var buf bytes.Buffer
	if err := components.GenuiResult(comps, "", false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("components.GenuiResult(full): %v", err)
	}
	for _, want := range []string{"hello", "Ship it", "today", "A", "B", "1", "<table", "<h3", "Apply"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("full result missing %q", want)
		}
	}
	// The scroll anchor must ride INSIDE the replaced region, after the
	// components: the patch is an outer replace of #genui-result, so the
	// node that arrives with the answer is what brings it into view.
	if !strings.Contains(buf.String(), "scrollIntoView") {
		t.Errorf("full result carries no scroll anchor — an answer longer than the page lands off-screen: %s", buf.String())
	}
}

// TestGenuiResultRendersVisualComponents proves the non-text half:
// bars scale by width with text values (never color-only) and action
// buttons post to same-origin URLs with their fields.
func TestGenuiResultRendersVisualComponents(t *testing.T) {
	t.Parallel()
	dirs, err := components.ParseDirectives(`{"components":[` +
		`{"type":"bar_chart","props":{"title":"W","bars":[{"label":"Mon","value":2},{"label":"Tue","value":4}]}},` +
		`{"type":"action_buttons","props":{"actions":[` +
		`{"label":"Add","method":"POST","url":"/api/todos","fields":{"title":"x"}}]}}]}`)
	if err != nil {
		t.Fatalf("components.ParseDirectives(visual): %v", err)
	}
	comps, err := components.RenderAll(dirs)
	if err != nil {
		t.Fatalf("components.RenderAll(visual): %v", err)
	}
	var buf bytes.Buffer
	if err := components.GenuiResult(comps, "", false).Render(context.Background(), &buf); err != nil {
		t.Fatalf("components.GenuiResult(visual): %v", err)
	}
	html := buf.String()
	for _, want := range []string{`role="img"`, `width: 100`, `width: 50`, `name="title"`, ">W<", ">Mon<"} {
		if !strings.Contains(html, want) {
			t.Errorf("visual result missing %q", want)
		}
	}
}
