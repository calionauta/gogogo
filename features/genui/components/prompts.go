// The model-facing instructions for the Chat answer shape.
// Split from catalog.go for the 500-line file gate: the registry decides
// what RENDERS, this file decides what the model is TOLD.
//
// The prompt is built from pieces (fragments), and the SURFACE SET
// decides which pieces appear — see surface.go. That is what lets an operator
// toggle the vocabulary rather than maintaining two hand-written prompts
// that drift.
package components

import (
	"strings"

	"github.com/calionauta/gogogo/internal/prompts"
)

// fragment returns the catalog line for one shared leaf name, or "" when the
// name is not a leaf (so callers can compose without knowing the registry
// order). Kept as a switch, not a map, so a component added to the registry
// without a fragment is a visible omission in review.
func leafFragment(name string) string {
	switch name {
	case textNoteName:
		return textNoteName + " {text}, "
	case planCardsName:
		// The action shape appears only when this build actually offers one:
		// advertising it with nothing declared invites an answer that names an
		// endpoint nobody declared, which the validator then rejects — the
		// whole answer lost for a field the prompt asked for. See actions.go.
		if len(DeclaredActions()) > 0 {
			return planCardsName + " {plans:[{title, detail, action?:" +
				"{label, method:POST|GET, url:<one declared below>, fields:{}}}]}, "
		}
		return planCardsName + " {plans:[{title, detail}]}, "
	case dataTableName:
		return dataTableName + " {headers:[], rows:[[]]}, "
	case barChartName:
		return barChartName + " {title, bars:[{label, value>=0}]}, "
	case actionButtonsName:
		if len(DeclaredActions()) > 0 {
			return actionButtonsName + " {actions:[{label, method:POST|GET, url:<one declared below>, fields:{}}]}, "
		}
		return ""
	case diagramName:
		return diagramName + " {source: mermaid code " +
			"(flowchart, sequenceDiagram, classDiagram, stateDiagram, " +
			"erDiagram, gantt, pie, mindmap, kanban, timeline, journey, " +
			"xychart, quadrantChart, ...), title?}, "
	}
	return ""
}

// catalogFragment lists the shared leaves, in the order the prompt has always
// advertised them. Membership comes from catalogLeafNames, so the registry and
// the prompt cannot disagree about the band.
func catalogFragment() string {
	var b strings.Builder
	for _, name := range catalogLeafNames {
		b.WriteString(leafFragment(name))
	}
	return b.String()
}

// commonFragment is what every leg advertises regardless of surface toggles:
// the catalog leaves and the one container. No html_block, no panels.
func commonFragment() string {
	return catalogFragment() + sectionFragment()
}

// bandFragments is the advertised component list, built band by band.
//
// html_block leads when the custom band is on, and that ORDER is load-bearing:
// with the catalog listed first the model answers with the nearest catalog
// component and the chat leg reads exactly like the catalog-only one (a user
// reported precisely that).
//
// section rides with the catalog band because nesting is a catalog affordance —
// with no leaves to nest, a container is noise.
func bandFragments(surfaces []Surface) string {
	var b strings.Builder
	if HasSurfaces(surfaces, SurfaceCustom) {
		b.WriteString("html_block {html: markup you invent for this answer}, ")
	}
	if HasSurfaces(surfaces, SurfaceCatalog) {
		b.WriteString(commonFragment())
	}
	return b.String()
}

// customGuidance is the invented-markup instructions. Advertised only with the
// custom surface, and led with on purpose where it is: at least one band must
// lead, so the guidance names the one the model is most likely to under-use.
func customGuidance(surfaces []Surface) string {
	if !HasSurfaces(surfaces, SurfaceCustom) {
		return ""
	}
	note := ""
	if HasSurfaces(surfaces, SurfaceCatalog) {
		// Trailing space keeps the sentence boundary when the note is spliced
		// into the middle of the guidance file.
		note = prompts.Text("genui/guidance-custom-catalog") + " "
	}
	return prompts.MustRender("genui/guidance-custom", map[string]string{
		"vocabulary":   ModelHTMLVocabulary(),
		"catalog_note": note,
	})
}

// PromptForSurfaces is the entry point: the SURFACE SET decides the
// vocabulary. Chat calls it with its configured set.
func PromptForSurfaces(surfaces []Surface) string {
	return promptForSurfaces(surfaces)
}

// PromptForCatalogLeadSurfaces is the router's variant: the catalog stays
// fully advertised on purpose (see router.go), and the lead hint is dropped
// when the pick is not in the enabled vocabulary.
func PromptForCatalogLeadSurfaces(lead string, surfaces []Surface) string {
	base := promptForSurfaces(surfaces)
	if lead == "" || !Registered(lead) || !advertisedAs(lead, surfaces) {
		return base
	}
	return base + " " + prompts.MustRender("genui/lead", map[string]string{"component": lead})
}

// PromptForChat is Chat's default surface: the catalog plus the invented-visual
// escape hatch.
//
// Chat answers with a component invented for THIS request; the model composes
// Tailwind/DaisyUI markup itself — sanitized server-side, so scripts, styles,
// handlers and media never reach the browser — and reaches for the catalog
// only where markup cannot act (a form posting to this app) or would render
// a fact worse than the catalog already does.
func PromptForChat() string {
	return promptForSurfaces(DefaultSurfacesChat)
}

// promptForSurfaces assembles one leg's instructions from the enabled surface
// bands. The envelope and the closing "no prose" rule are shared; the middle is
// the surface set.
//
// An empty set is prose-only: no band is enabled, so there is no envelope to
// fill and the model answers in plain prose (the worker degrades whatever
// arrives to its text fallback). This is what keeps all-three-off a working
// chat instead of a prompt that names zero components and still demands JSON.
//
// NormalizeSurfaces runs here (and in CatalogComponentsFor) so a nil set
// means the Chat default for every caller, including one that passes a raw
// set — the invariant lives in the normalizer, not in each constant being right.
func promptForSurfaces(surfaces []Surface) string {
	surfaces = NormalizeSurfaces(surfaces)
	if len(surfaces) == 0 {
		return prompts.Text("genui/answer-prose")
	}
	return joinFragments([]string{
		prompts.MustRender("genui/envelope", map[string]string{"components": bandFragments(surfaces)}),
		customGuidance(surfaces),
		panelsFragment(surfaces),
		panelsGuidance(surfaces),
		smalltalkRule(surfaces),
		actionsFragmentIfAllowed(surfaces),
		dataRule(surfaces),
		prompts.Text("genui/closing-rule"),
	})
}

// joinFragments assembles the prompt from the fragments that apply, with one
// space between them. The prompt text itself lives in .prompt files; this
// function only decides which files' output participates, so an operator
// toggling a surface cannot leave a dangling sentence behind.
func joinFragments(parts []string) string {
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(part)
	}
	return b.String()
}

// smalltalkRule keeps greetings from becoming data answers. The model
// reaches for panels whenever they are advertised — asked "hi" with a panel
// in its vocabulary it renders the user's own rows (live report) — so the
// prompt states the negative explicitly, next to the panel guidance it
// constrains. It names no component on purpose: the rule must hold for every
// surface set, including ones where text_note itself is not advertised.
func smalltalkRule(surfaces []Surface) string {
	if !HasSurfaces(surfaces, SurfaceNative) || len(panelNames()) == 0 {
		return ""
	}
	return "Greetings, thanks and other smalltalk (hi, hello, hey, thanks, ok, " +
		"goodbye) are never about the user's data: answer them briefly and " +
		"never emit a panel or a data component for them. "
}

// advertisedAs reports whether a catalog name is inside the enabled vocabulary.
// It is the single arbiter for "may the model be told this exists", consulted
// by the prompt and by the router's lead hint — so a name can never be hinted
// at a leg whose prompt does not advertise it.
func advertisedAs(name string, surfaces []Surface) bool {
	if IsPanel(name) {
		// Panels are native: advertised exactly when the native band is on.
		return HasSurfaces(surfaces, SurfaceNative)
	}
	if name == htmlBlockName {
		return HasSurfaces(surfaces, SurfaceCustom)
	}
	return HasSurfaces(surfaces, SurfaceCatalog)
}

// sectionFragment is the container's line: it is part of the catalog, not a
// panel, so it is spelled out rather than registered.
func sectionFragment() string {
	return prompts.Text("genui/section")
}

// panelsFragment lists the registered system panels after the catalog. A build
// with no panels wired, or with the native surface off, advertises none.
func panelsFragment(surfaces []Surface) string {
	if !HasSurfaces(surfaces, SurfaceNative) {
		return ""
	}
	names := panelNames()
	if len(names) == 0 {
		return ""
	}
	return prompts.MustRender("genui/panels-list", map[string]string{"panels": strings.Join(names, ", ")})
}

// panelsGuidance is where each panel says when to reach for it: the text
// lives with the component, so a panel cannot ship without its prompt line.
//
// The opening sentence is a RULE, not a hint, and it is deliberately blunt.
// Both legs used to end with a soft "reach for the app's OWN data when the
// request is about it", which the model read as one option among several:
// asked to "plan my days from to-dos" it invented todo titles in a data_table
// instead of rendering the user's real list (live report). Inventing the
// user's own data is the failure the panel exists to prevent, so the prompt
// states the prohibition and the required answer shape, and each panel's own
// line says what it shows.
func panelsGuidance(surfaces []Surface) string {
	if !HasSurfaces(surfaces, SurfaceNative) {
		return ""
	}
	names := panelNames()
	if len(names) == 0 {
		return ""
	}
	fragments := make([]string, 0, len(names))
	for _, name := range names {
		fragments = append(fragments, panelPrompt(name))
	}
	return prompts.MustRender("genui/guidance-panels", map[string]string{
		"panels": strings.Join(fragments, "; "),
	})
}

// dataRule is the same prohibition compressed to one line, appended LAST by
// promptForSurfaces so it lands immediately before the user's request. Split
// out because placement is the point: mid-prompt guidance is exactly what a
// long vocabulary beats into background.
func dataRule(surfaces []Surface) string {
	if !HasSurfaces(surfaces, SurfaceNative) || len(panelNames()) == 0 {
		return ""
	}
	return prompts.Text("genui/rule-data")
}
