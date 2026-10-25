package main

import (
	"strings"
	"testing"
)

// RED-proof for direction 1: a class in markup that no stylesheet defines.
// This is the `.ui-field-group` failure: four pages used it, nothing styled it,
// and every other check in the repo passed.
func TestUsedButUndefinedClassFails(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor(),
		"features/x/components/page.templ": `<div class="ui-phantom-grid"></div>`,
	})
	got := findingsFor(t, root)
	if !strings.Contains(got, `class "ui-phantom-grid" is used but not defined`) {
		t.Fatalf("expected the invented class to fail, got:\n%s", got)
	}
}

// GREEN: the same class defined and used is clean — no false positive.
func TestDefinedAndUsedIsClean(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css": cssFor("ui-page", "ui-title", "app-card"),
		"features/x/components/page.templ": `<div class="ui-page">` +
			`<div class="ui-title"></div><div class="app-card"></div></div>`,
	})
	if got := findingsFor(t, root); got != "" {
		t.Fatalf("expected a clean tree, got:\n%s", got)
	}
}

// RED-proof for direction 3: a selector nothing references. This is the
// `.app-theme-section` residue — the footer's theme section moved into the
// account panel and its rules stayed behind.
func TestDeadSelectorFails(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("app-card") + ".app-theme-section { display: flex; }\n",
		"features/x/components/page.templ": `<div class="app-card"></div>`,
	})
	got := findingsFor(t, root)
	if !strings.Contains(got, `.app-theme-section is defined but used nowhere`) {
		t.Fatalf("expected dead CSS to fail, got:\n%s", got)
	}
}

// A store-only reference still counts as a use: state classes are toggled from
// a script, never written in markup.
func TestScriptReferenceKeepsSelectorAlive(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("app-card") + ".is-open { transform: rotate(90deg); }\n",
		"web/resources/static/panel.js":    `el.classList.toggle("is-open");`,
		"features/x/components/page.templ": `<div class="app-card"></div>`,
	})
	if got := findingsFor(t, root); got != "" {
		t.Fatalf("a script-only reference must keep the selector alive, got:\n%s", got)
	}
}

// RED-proof for direction 2: the kit table promises a class the CSS does not
// ship. Four phantom rows survived a migration this way.
func TestDocumentedButUndefinedFails(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("app-card"),
		"features/x/components/page.templ": `<div class="app-card"></div>`,
		"docs/ui-shell.md":                 "| Class | Use |\n|---|---|\n| `.ui-btn` | the only button shapes |\n",
	})
	got := findingsFor(t, root)
	if !strings.Contains(got, "documented class .ui-btn is not defined") {
		t.Fatalf("expected the phantom doc row to fail, got:\n%s", got)
	}
}

// RED-proof for the DaisyUI part rules: the component name on the wrong
// element, or a child without its container, is invisible to every other gate.
func TestDaisyPartsAreEnforced(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		markup string
		want   string
	}{
		{
			name:   "toggle on a button",
			markup: `<div class="app-card"><button class="toggle toggle-sm" type="button"></button></div>`,
			want:   `daisyUI "toggle" belongs on an <input>`,
		},
		{
			name:   "join-item without join",
			markup: `<div class="app-card"><input class="input join-item"/></div>`,
			want:   `daisyUI part "join-item" needs its "join" container`,
		},
		{
			name:   "dropdown-content without dropdown",
			markup: `<div class="app-card"><ul class="dropdown-content"></ul></div>`,
			want:   `daisyUI part "dropdown-content" needs its "dropdown" container`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := writeTree(t, map[string]string{
				"web/resources/static/chrome.css":  cssFor("app-card"),
				"features/x/components/page.templ": tc.markup,
			})
			if got := findingsFor(t, root); !strings.Contains(got, tc.want) {
				t.Fatalf("expected %q, got:\n%s", tc.want, got)
			}
		})
	}
}

// The correct protocol is clean: the gate must not punish the right markup.
func TestCorrectDaisyPartsAreClean(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css": cssFor("app-card"),
		"features/x/components/page.templ": `<div class="app-card join">
	<input class="input join-item" type="checkbox"/>
	<input type="checkbox" class="toggle toggle-sm"/>
	<details class="dropdown"><ul class="dropdown-content"></ul></details>
</div>`,
	})
	if got := findingsFor(t, root); got != "" {
		t.Fatalf("expected the correct protocol to pass, got:\n%s", got)
	}
}

// The extractor must read class EXPRESSIONS, not just quoted attributes: the
// demo toggle and the account row both pass classes through templ.Classes, and
// an extractor that only understood class="..." would silently skip the state
// modifiers inside them. Both literals must reach the checker, so with an empty
// stylesheet they have to be REPORTED, which is the proof they were seen at all.
func TestClassExpressionIsExtracted(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css": cssFor(),
		"features/x/components/page.templ": `<a class={ templ.Classes("app-demo-toggle", ` +
			`map[string]bool{"is-on": on}) }>x</a>`,
	})
	got := findingsFor(t, root)
	wants := []string{
		`class "app-demo-toggle" is used but not defined`,
		`class "is-on" is used but not defined`,
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q (the expression's literals must be extracted), got:\n%s", want, got)
		}
	}
}

// data-class is a Datastar expression, not the static vocabulary, and the
// gate must not mistake it for one.
func TestDataClassIsIgnored(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("app-card"),
		"features/x/components/page.templ": `<div class="app-card" data-class="{'app-not-real': cond}"></div>`,
	})
	if got := findingsFor(t, root); got != "" {
		t.Fatalf("data-class must be ignored, got:\n%s", got)
	}
}

// A comment that names a class is not a definition: the `.app-x { }` inside a
// prose block was how a dead selector could look alive.
func TestCommentIsNotADefinition(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  "/* .app-ghost was removed */\n" + cssFor("app-card"),
		"features/x/components/page.templ": `<div class="app-card app-ghost"></div>`,
	})
	got := findingsFor(t, root)
	if !strings.Contains(got, `class "app-ghost" is used but not defined`) {
		t.Fatalf("a comment must not define a class, got:\n%s", got)
	}
}
