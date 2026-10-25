package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fillerComponents pads the fixture skill past minComponents, so a test never
// has to spell out 40 components. The real ones appended below are the shapes
// the derived rules are exercised on.
const fillerComponents = 41

// fence renders one fenced HTML sample.
func fence(html string) string {
	return "```html\n" + html + "\n```\n\n"
}

// component renders one skill section in the vendored format.
func component(name, prose, classNames, syntax string) string {
	return "### " + name + "\n\n" + prose + "\n\n#### Class names\n" + classNames +
		"\n#### Syntax\n" + fence(syntax)
}

// fixtureSkill is a small, valid skill in the vendored shape: a component
// bullet per section, a syntax sample that declares the host element.
func fixtureSkill() string {
	var b strings.Builder
	b.WriteString("# daisyUI 5\n\n## daisyUI components\n\n")
	for i := range fillerComponents {
		name := fmt.Sprintf("filler%d", i)
		fmt.Fprintf(&b, "### Filler %d\n\n#### Class names\n- component: `%s`\n\n", i, name)
		fmt.Fprintf(&b, "#### Syntax\n%s", fence(`<div class="`+name+`"></div>`))
	}
	b.WriteString(component("Toggle", "A toggle is a checkbox with the style of a switch.",
		"- component: `toggle`\n- color: `toggle-primary`\n- size: `toggle-xs`, `toggle-sm`\n",
		`<input type="checkbox" class="toggle {MODIFIER}" />`))
	b.WriteString(component("Join", "Join groups a field and its action.",
		"- component: `join`\n- part: `join-item`\n",
		`<div class="join"><input class="join-item" /><button class="join-item"></button></div>`))
	b.WriteString(component("Dropdown", "A dropdown opens content when clicked.",
		"- component: `dropdown`\n- part: `dropdown-content`\n",
		`<details class="dropdown"><summary></summary><ul class="dropdown-content"></ul></details>`))
	// Input appears on two elements across the document (its own section and
	// the Label one), which is the shape the host rule must tolerate.
	b.WriteString(component("Label", "A label names a field.",
		"- component: `label`\n",
		`<label class="input"><span class="label">{text}</span><input type="text" /></label>`))
	b.WriteString(component("Input", "A text field.",
		"- component: `input`\n- size: `input-sm`, `input-lg`\n",
		`<input type="text" class="input {MODIFIER}" />`))
	return b.String()
}

// fixtureRegistry is an empty, valid registry: a test that does not care about
// it must not fail on it.
func fixtureRegistry() string {
	return `{"version": 1, "entries": []}`
}

// fixtureCompositions is the human half, with no numbered heading.
func fixtureCompositions() string {
	return "# UI compositions\n\nNo compositions here.\n"
}

// writeTree materialises a checkout skeleton the checker can walk. The skill,
// the registry and the compositions doc are added when a test does not supply
// its own, so each test spells only what it is about.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	defaults := map[string]string{
		".agents/skills/daisyui/SKILL.md": fixtureSkill(),
		"internal/ui/registry.json":       fixtureRegistry(),
		"docs/ui-compositions.md":         fixtureCompositions(),
	}
	for rel, body := range defaults {
		if _, ok := files[rel]; !ok {
			files[rel] = body
		}
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// findingsFor returns the messages joined, so a test can assert on substrings
// without caring which direction produced them.
func findingsFor(t *testing.T, root string) string {
	t.Helper()
	found, err := checkTree(root, true)
	if err != nil {
		t.Fatalf("checkTree: %v", err)
	}
	var b strings.Builder
	for _, f := range found {
		b.WriteString(f.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// cssFor returns a stylesheet that defines exactly the classes a fixture uses,
// so the dead-selector direction cannot fire on the fixture itself.
func cssFor(classes ...string) string {
	var b strings.Builder
	for _, c := range classes {
		b.WriteString("." + c + " { display: block; }\n")
	}
	return b.String()
}

// writeBare materialises a tree with ONLY what the caller provides: no skill, no
// registry, no compositions doc. It is the consumer path, where the gate must
// degrade instead of refusing to run.
func writeBare(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// hasFinding reports whether any finding's message contains every substring.
func hasFinding(findings []finding, substrings ...string) bool {
	for _, f := range findings {
		ok := true
		for _, s := range substrings {
			if !strings.Contains(f.msg, s) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
