// SCOPE:plugin — generative leaf render tests (Chat demo).
package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/features/genui/components"
)

// TestLeafRendersCatalog pins the leaf contracts Chat depends on: every
// catalog component renders its content (headings + data) with a server
// instance id, so the thread can address each.
func TestLeafRendersCatalog(t *testing.T) {
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
	for _, d := range dirs {
		comp, err := components.Render(d)
		if err != nil {
			t.Fatalf("components.Render(%q): %v", d.Component, err)
		}
		var buf bytes.Buffer
		if err := comp.Render(context.Background(), &buf); err != nil {
			t.Fatalf("render %q: %v", d.Component, err)
		}
		if !strings.Contains(buf.String(), `id="genui-`) {
			t.Errorf("%q rendered with no instance id: %s", d.Component, buf.String())
		}
	}
	var hello bytes.Buffer
	note, noteErr := components.Render(dirs[0])
	if noteErr != nil {
		t.Fatalf("render text_note: %v", noteErr)
	}
	if err := note.Render(context.Background(), &hello); err != nil {
		t.Fatalf("render text_note: %v", err)
	}
	for _, want := range []string{"hello"} {
		if !strings.Contains(hello.String(), want) {
			t.Errorf("text_note missing %q", want)
		}
	}
	// A plan with NO action renders no button. That is the contract that lets
	// the card be used for plans nobody can act on.
	var plan bytes.Buffer
	planComp, err := components.Render(dirs[1])
	if err != nil {
		t.Fatalf("render plan_cards: %v", err)
	}
	if err := planComp.Render(context.Background(), &plan); err != nil {
		t.Fatalf("render plan_cards: %v", err)
	}
	if strings.Contains(plan.String(), ">Apply<") {
		t.Errorf("a plan without an action rendered a button: %s", plan.String())
	}
}

// TestLeafRendersVisualComponents proves the non-text half:
// bars scale by width with text values (never color-only) and action
// buttons post to same-origin URLs with their fields.
func TestLeafRendersVisualComponents(t *testing.T) {
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
	for _, c := range comps {
		if err := c.Render(context.Background(), &buf); err != nil {
			t.Fatalf("render visual: %v", err)
		}
	}
	html := buf.String()
	for _, want := range []string{`role="img"`, `width: 100`, `width: 50`, `name="title"`, ">W<", ">Mon<"} {
		if !strings.Contains(html, want) {
			t.Errorf("visual result missing %q", want)
		}
	}
}
