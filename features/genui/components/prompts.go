// SCOPE:plugin — the model-facing instructions for both answer shapes.
// Split from catalog.go for the 500-line file gate: the registry decides
// what RENDERS, this file decides what the model is TOLD.
//
// Both legs are built from the same pieces (fragments), and the SURFACE SET
// decides which pieces appear — see surface.go. That is what lets the two legs
// differ by mode and by an operator toggle rather than by two hand-maintained
// prompts that drift.
package components

import "strings"

// envelopePrefix is the answer shape every leg asks for, spelled once so the
// two prompts cannot disagree on the wire format.
const envelopePrefix = "Answer with a single JSON object shaped " +
	`{"components":[{"type":"<name>","props":{...}}]} ` +
	"using ONLY these components: "

// fragment returns the catalog line for one shared leaf name, or "" when the
// name is not a leaf (so callers can compose without knowing the registry
// order). Kept as a switch, not a map, so a component added to the registry
// without a fragment is a visible omission in review.
func leafFragment(name string) string {
	switch name {
	case textNoteName:
		return textNoteName + " {text}, "
	case planCardsName:
		return planCardsName + " {plans:[{title, detail}]}, "
	case dataTableName:
		return dataTableName + " {headers:[], rows:[[]]}, "
	case barChartName:
		return barChartName + " {title, bars:[{label, value>=0}]}, "
	case actionButtonsName:
		return actionButtonsName + " {actions:[{label, method:POST|GET, url:/same/origin/path, fields:{}}]}, "
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
	guidance := "Lead with html_block: build the visual this request asks for out of " +
		"plain elements (div, span, p, headings, lists, table) and these " +
		"classes ONLY -- a class outside this list renders unstyled: " +
		ModelHTMLVocabulary() + ". Prefer that visual over reaching for the " +
		"nearest catalog component, and over answering in prose. "
	if HasSurfaces(surfaces, SurfaceCatalog) {
		guidance += "Use the catalog where markup cannot act: action_buttons to " +
			"post to this app, text_note for a single line of text. "
	}
	return guidance + "Only structure and classes survive rendering: " +
		"script, style, inline style attributes, event handlers, forms, media, " +
		"svg and images are stripped, so put every style in a class. "
}

// PromptForCatalog is the catalog-only leg (Ask's default).
func PromptForCatalog() string {
	return promptForSurfaces(DefaultSurfacesAsk)
}

// PromptForCatalogSurfaces is the catalog leg's prompt for an explicit surface
// set — the entry AskPrompt uses, with PromptForCatalog as the default-set
// convenience.
func PromptForCatalogSurfaces(surfaces []Surface) string {
	return promptForSurfaces(surfaces)
}

// PromptForSurfaces is the shared entry point: both legs build their prompt
// through here, so the SURFACE SET — not the leg — decides the vocabulary.
func PromptForSurfaces(surfaces []Surface) string {
	return promptForSurfaces(surfaces)
}

// PromptForCatalogLead is the router's variant: the catalog stays fully
// advertised on purpose (see router.go), and the lead hint is dropped when the
// pick is not in the enabled vocabulary — including a chat-only name, which is
// what keeps a classifier from reaching the invented-markup surface.
func PromptForCatalogLead(lead string) string {
	return PromptForCatalogLeadSurfaces(lead, DefaultSurfacesAsk)
}

// PromptForCatalogLeadSurfaces is PromptForCatalogLead for an explicit surface
// set.
func PromptForCatalogLeadSurfaces(lead string, surfaces []Surface) string {
	base := promptForSurfaces(surfaces)
	if lead == "" || !Registered(lead) || !advertisedAs(lead, surfaces) {
		return base
	}
	return base + " Lead with " + lead + " as the top-level component."
}

// PromptForChat is Chat's default surface: the catalog plus the invented-visual
// escape hatch.
//
// Ask demonstrates answers shaped by a fixed catalog, chat answers with a
// component invented for THIS request; the model composes Tailwind/DaisyUI
// markup itself — sanitized server-side, so scripts, styles, handlers and
// media never reach the browser — and reaches for the catalog only where
// markup cannot act (a form posting to this app) or would render a fact worse
// than the catalog already does.
func PromptForChat() string {
	return promptForSurfaces(DefaultSurfacesChat)
}

// promptForSurfaces assembles one leg's instructions from the enabled surface
// bands. The envelope and the closing "no prose" rule are shared; the middle is
// the surface set.
//
// NormalizeSurfaces runs here (and in CatalogComponentsFor) so "native is
// always on" holds for every caller, including one that passes a raw set —
// the invariant lives in the normalizer, not in each constant being right.
func promptForSurfaces(surfaces []Surface) string {
	surfaces = NormalizeSurfaces(surfaces)
	return envelopePrefix +
		bandFragments(surfaces) +
		customGuidance(surfaces) +
		panelsFragment(surfaces) +
		panelsGuidance(surfaces) +
		"No prose outside the JSON object."
}

// advertisedAs reports whether a catalog name is inside the enabled vocabulary.
// It is the single arbiter for "may the model be told this exists", consulted
// by the prompt and by the router's lead hint — so a name can never be hinted
// at a leg whose prompt does not advertise it.
func advertisedAs(name string, surfaces []Surface) bool {
	if IsPanel(name) {
		// Panels are native, and native is always on (NormalizeSurfaces).
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
	return "section {title, children:[<component>]} holding nested components. "
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
	return "Panels (the app's own data, always available): " + strings.Join(names, ", ") + ". "
}

// panelsGuidance is where each panel says when to reach for it: the text
// lives with the component, so a panel cannot ship without its prompt line.
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
	return "Reach for the app's OWN data when the request is about it (" +
		strings.Join(fragments, "; ") + "). "
}
