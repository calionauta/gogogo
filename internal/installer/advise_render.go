// SCOPE:plugin — installer engine: advise rendering (one text shape per scope).
// Split from advise.go so the resolver and the renderers each stay under
// the file-sizes gate, and so a new scope has one obvious place to land.
package installer

import (
	"fmt"
	"strings"
)

// renderExceptionToGo is the exception-to-go-scope text: the gate, the skill
// pointer, the normative doc. No trim mechanics, no capability table, no
// presets — a kernel need matches template vocabulary only by accident, and
// printing it would invite installing web units into a codec.
// Split out so Advise stays under the gocyclo gate.
func renderExceptionToGo(doc adviseDoc) string {
	var b strings.Builder
	b.WriteString("exception to Go (Zig) — the gogogo template does not install here.\n" +
		"Zig is this repo's one documented native exception, so the opinion " +
		"below is the gate, not a foreign-stack disclaimer.\n\n")
	b.WriteString("rules:\n")
	for _, r := range doc.Rules {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	renderNext(&b, doc)
	return b.String()
}

// renderStdlib is the go-standards-scope text: no capability table, no trim
// mechanics, no presets — just the rules that survive without the template.
//
// It renders doc.Rules rather than a fixed list, because two different
// conditions reach this scope (the need forbids dependencies, or --dir is not a
// checkpoint) and each ships its own rule set. Printing the header from
// doc.Reason keeps the explanation matched to the cause.
// Split out so Advise stays under the gocyclo gate, like renderExceptionToGo.
func renderStdlib(doc adviseDoc) string {
	var b strings.Builder
	switch doc.Reason {
	case reasonNotCheckout:
		b.WriteString("the path is not a gogogo checkout — the template does not apply here.\n" +
			"No capability is installable into it: this tool trims and extends its own scaffold,\n" +
			"not an arbitrary project.\n\n")
	default:
		b.WriteString("dependency-constrained need — the gogogo template does not apply here.\n" +
			"No capability is installable: each one adds or belongs to a dependency\n" +
			"this need forbids, so the registry is omitted rather than shown empty.\n\n")
	}
	b.WriteString("rules:\n")
	for _, r := range doc.Rules {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	renderNext(&b, doc)
	return b.String()
}

// renderTemplate is the template-scope text: rules, keep/drop presets with
// the no-match recovery hint, the full capability table, and the scaffold
// first-run (withheld when a non-empty need matched nothing — a miss must
// not invite scaffolding the wrong thing).
// Split out so Advise stays under the gocyclo gate, like the other renders.
// renderNext prints the route: what to run, in order, and why. An empty route
// prints nothing — an answer with neither a stated nor a detected intent must
// not invent one. A step with no command is advice only (a foreign tree has no
// recorded base to compare, so the sentence is the whole step).
func renderNext(b *strings.Builder, doc adviseDoc) {
	if len(doc.Next) == 0 {
		return
	}
	fmt.Fprintf(b, "\nnext (intent: %s, %s):\n", doc.Intent, doc.IntentSource)
	for i, s := range doc.Next {
		if s.Run == "" {
			fmt.Fprintf(b, "  %d. %s\n", i+1, s.Why)
			continue
		}
		fmt.Fprintf(b, "  %d. %s\n     why: %s\n", i+1, s.Run, s.Why)
	}
}

// renderPresets prints the matched presets, or the recovery hint when none
// matched. Shared by the template and foreign-tree scopes: which units fit a
// use case does not depend on the tree the reader happens to sit in.
func renderPresets(b *strings.Builder, doc adviseDoc, need string) {
	if len(doc.Presets) == 0 {
		names := make([]string, 0, len(advisePresets))
		for _, p := range advisePresets {
			names = append(names, p.Name)
		}
		fmt.Fprintf(b, "  no preset matched %q — available: %s\n", need, strings.Join(names, ", "))
		fmt.Fprintf(b, "  %s\n", notApplicableHint)
		return
	}
	for _, p := range doc.Presets {
		fmt.Fprintf(b, "  %s: keep [%s]", p.Name, strings.Join(p.Keep, ", "))
		if len(p.Drop) > 0 {
			fmt.Fprintf(b, " drop [%s]", strings.Join(p.Drop, ", "))
		}
		fmt.Fprintf(b, "\n    %s\n", p.Note)
	}
}

// renderForeignTree is the foreign-tree scope: the path is not a gogogo
// checkout, so nothing INSTALLS there — `add` and `new` rewire a scaffold's own
// router and registry and refuse rather than guess at someone else's. What the
// reader can still do is TAKE a unit, which is why this scope prints the
// capability table with its owned paths: the copy column is the unit swap
// surface `add` cannot provide outside a scaffold, and the route turns it into
// a scratch export plus the porting checklist.
func renderForeignTree(doc adviseDoc, need string) string {
	var b strings.Builder
	b.WriteString("foreign tree — the path given with --dir is not a gogogo checkout.\n")
	b.WriteString("Nothing installs here (add and new rewire the scaffold's own router and\n")
	b.WriteString("registry). The copy column below is what you would TAKE; the route is\n")
	b.WriteString("the scratch export plus the porting checklist.\n")
	renderNext(&b, doc)
	b.WriteString("\nrules:\n")
	for _, r := range doc.Rules {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	if len(doc.Presets) > 0 {
		b.WriteString("\npresets (use-case → keep/copy):\n")
		renderPresets(&b, doc, need)
	}
	b.WriteString("\ncapabilities (id, kind, what you would copy):\n")
	for _, c := range doc.Capabilities {
		fmt.Fprintf(&b, "  %-14s %-7s %s\n", c.ID, c.Kind, c.Summary)
		if len(c.Dirs)+len(c.Files) > 0 {
			fmt.Fprintf(&b, "  %-14s         copy: %s\n", "",
				strings.Join(append(c.Dirs, c.Files...), ", "))
		}
	}
	return b.String()
}

func renderTemplate(doc adviseDoc, need string) string {
	var b strings.Builder
	b.WriteString("gogogo advise — opinions, not changes (nothing was installed):\n\n")
	b.WriteString("rules:\n")
	for _, r := range doc.Rules {
		fmt.Fprintf(&b, "  - %s\n", r)
	}
	renderNext(&b, doc)
	b.WriteString("\npresets (use-case → keep/drop):\n")
	renderPresets(&b, doc, need)
	b.WriteString("\ncapabilities (id, kind, how to switch each off):\n")
	for _, c := range doc.Capabilities {
		fmt.Fprintf(&b, "  %-14s %-7s %s\n", c.ID, c.Kind, c.Summary)
		fmt.Fprintf(&b, "  %-14s         trim: %s", "", c.Trim)
		if c.RuntimeOff != "" {
			fmt.Fprintf(&b, " | off: %s", c.RuntimeOff)
		}
		b.WriteString("\n")
		if len(c.Dirs)+len(c.Files) > 0 {
			fmt.Fprintf(&b, "  %-14s         copy: %s\n", "",
				strings.Join(append(c.Dirs, c.Files...), ", "))
		}
	}
	if n := doc.FirstRun; n != nil {
		b.WriteString("\nfirst run (defaults; PORT overrides the port):\n")
		fmt.Fprintf(&b, "  %s\n", strings.Replace(n.Dev, "<dir>", "my-app", 1))
		fmt.Fprintf(&b, "  app %s · login %s\n", n.App, n.Login)
		fmt.Fprintf(&b, "  admin %s\n", n.Admin)
		fmt.Fprintf(&b, "  workflows %s\n", n.Flows)
	}
	return b.String()
}
