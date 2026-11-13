// Installer engine: the native panel template and the prompt it reads.
//
// The panel is the slice's own rows, answerable from Chat. Its sentence is
// model-facing, so it lives in the prompt library: the scaffold writes the file
// and the panel loads it by id, so the two cannot disagree.
//
// Split from new_scaffold.go for the file-size gate.
package installer

import "strings"

// newPanelPromptSource renders the prompt file the scaffolded feature's panel
// reads: internal/prompts/files/panels/<name>.prompt.
//
// The panel's sentence is model-facing, so it lives with the other prompts
// rather than inside the feature's Go code. The scaffold writes the file and
// the panel loads it by id, so the two can never disagree — and the
// check-prompts gate sees a prompt file with no reference if one is renamed.
func newPanelPromptSource(name string) string {
	return strings.NewReplacer(
		"{{name}}", name,
		"{{text}}", panelPromptText(name),
	).Replace(newPanelPromptTemplate)
}

// panelPromptText is the sentence the panel advertises: the same one the
// prompt file carries and the receipt pins, spelled once.
func panelPromptText(name string) string {
	return name + "_list {scope: all|mine} whenever the request involves the user's " + name + " data"
}

// newPanelPromptTemplate is the prompt file the scaffold writes.
const newPanelPromptTemplate = `---
name: panels/{{name}}
description: "Panel line: when the model should answer from the {{name}} slice's own rows."
version: 1
output: json
variables: []
---
{{text}}
`

// newPanelSource renders features/<name>/handlers/genui_panel.go: the
// SlicePanel skeleton. The two closures are the whole recipe-specific
// surface — List (owner-scoped rows) and View (the page's own component).
func newPanelSource(name, module string) string {
	return strings.NewReplacer(
		"{{namePascal}}", pascalName(name),
		"{{name}}", name,
		"{{module}}", module,
	).Replace(newPanelTemplate)
}

// pascalName renders {{namePascal}} for the panel skeleton.
func pascalName(name string) string {
	if name == "" {
		return name
	}
	r := []rune(name)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 32
	}
	return string(r)
}

// newPanelTemplate is the genui panel skeleton. It implements the
// PanelProvider interface via comp.SlicePanel (the shared base), so a
// scaffolded recipe only writes its two closures.
// allow-inline-prompt: Go source template for a scaffolded recipe (codegen), not model instructions
const newPanelTemplate = `// The {{name}} feature's native panel: one file that makes its own
// data answerable from Chat. Render returns the feature's OWN component
// (never a chat-specific copy); Fill owns the rows; the model picks
// only the scope. Bult on comp.SlicePanel, the shared base.
package handlers

import (
	"context"
	"fmt"

	"github.com/a-h/templ"

	"{{module}}/internal/prompts"
)

// {{namePascal}}Panel is the {{name}}_list provider.
type {{namePascal}}Panel struct {
	// wire the store/queries here when the list grows beyond the skeleton
}

func (p *{{namePascal}}Panel) Component() string { return "{{name}}_list" }

func (p *{{namePascal}}Panel) Prompt() string {
	return prompts.Text("panels/{{name}}")
}

func (p *{{namePascal}}Panel) HomePage() string { return "/{{name}}" }

func (p *{{namePascal}}Panel) Aliases() []string { return []string{"{{name}}"} }

func (p *{{namePascal}}Panel) Writable() []string { return []string{"scope"} }

func (p *{{namePascal}}Panel) Fill(ctx context.Context, owner string, props map[string]any) (map[string]any, error) {
	scope, _ := props["scope"].(string)
	if scope != "all" && scope != "mine" {
		return nil, fmt.Errorf("{{name}} panel: scope %q is not one of [all mine]", scope)
	}
	// TODO: list the owner-scoped rows here (scope=mine filters owner).
	rows, err := p.list(ctx, owner, scope)
	if err != nil {
		return nil, err
	}
	return map[string]any{"scope": scope, "rows": rows}, nil
}

func (p *{{namePascal}}Panel) Render(props map[string]any) (templ.Component, error) {
	scope, _ := props["scope"].(string)
	if scope != "all" && scope != "mine" {
		return nil, fmt.Errorf("{{name}} panel: scope %q is not one of [all mine]", scope)
	}
	// TODO: return the feature's OWN list component, e.g.
	// components.{{namePascal}}List(rows...).
	return nil, fmt.Errorf("{{name}} panel: render your page component here")
}

func (p *{{namePascal}}Panel) list(ctx context.Context, owner string, scope string) (any, error) {
	_ = scope
	return nil, nil
}
`
