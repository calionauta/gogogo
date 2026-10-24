// SCOPE:plugin — the operator-facing answer catalog: what the model may
// emit, in one list. Split from catalog.go for the file-size gate: the
// registry decides what RENDERS, this file decides what the /config page
// SHOWS about it.
//
// The blurbs are hand-written one-liners, not parsed from leafFragment: the
// prompt line documents the wire shape for the model, the blurb explains the
// component for the operator, and conflating them made both worse to read.
// TestCatalogOverviewCoversRegistry pins that every registered name appears
// here, so a new component cannot land in the prompt but miss the page.
package components

// ComponentKind names the band a component belongs to, in the operator's
// words (the same three surfaces the toggles and GENUI_CHAT_SURFACES speak,
// plus the section container that rides with the catalog).
type ComponentKind string

const (
	// KindCatalog is a fixed-registry leaf (typed props, static content).
	KindCatalog ComponentKind = "catalog"
	// KindContainer is the section: groups nested components, catalog band.
	KindContainer ComponentKind = "container"
	// KindCustom is html_block: model-invented, sanitized markup.
	KindCustom ComponentKind = "custom"
	// KindNative is an app-owned panel: the model picks the slice, the app
	// fills real rows.
	KindNative ComponentKind = "native"
)

// ComponentInfo is one row of the answer catalog: the registry name the
// model writes, the band it belongs to, and what it is for.
type ComponentInfo struct {
	Name string
	Kind ComponentKind
	// Blurb is one operator-facing line. For native panels it is the
	// provider's own Prompt (the sentence that teaches the model when to
	// pick it); for generative components it is a fixed line kept next to
	// the registry so the two are reviewed together.
	Blurb string
	// Writable lists the props the model may set. Empty for catalog leaves
	// (the model fills their documented props) and populated for native
	// panels (the slice choice — never the rows).
	Writable []string
}

// catalogBlurbs is the operator line per generative component, in
// catalogLeafNames order plus the container and the custom band. Kept next
// to the registry so a rename touches both in one review.
var catalogBlurbs = []ComponentInfo{
	{Name: textNoteName, Kind: KindCatalog, Blurb: "A single prose card."},
	{Name: planCardsName, Kind: KindCatalog, Blurb: "Plan cards with an optional apply button (declared actions only)."},
	{Name: dataTableName, Kind: KindCatalog, Blurb: "A small read-only table."},
	{Name: barChartName, Kind: KindCatalog, Blurb: "A pure-CSS bar chart, no JavaScript."},
	{Name: actionButtonsName, Kind: KindCatalog, Blurb: "Buttons posting to this app (declared actions only)."},
	{Name: diagramName, Kind: KindCatalog, Blurb: "A Mermaid diagram rendered in the browser."},
	{Name: sectionName, Kind: KindContainer, Blurb: "Groups nested components under a title."},
	{Name: htmlBlockName, Kind: KindCustom, Blurb: "Markup the model invents for the answer, sanitized server-side."},
}

// CatalogOverview lists every answer component the model may emit: the
// generative catalog first (stable order), then the installed native panels
// (sorted). An install with no panels lists none — the table says "none
// installed" instead of implying the vocabulary is empty.
func CatalogOverview() []ComponentInfo {
	out := make([]ComponentInfo, 0, len(catalogBlurbs)+len(panelNames()))
	out = append(out, catalogBlurbs...)
	for _, name := range panelNames() {
		p := panelProvider(name)
		if p == nil {
			continue
		}
		out = append(out, ComponentInfo{
			Name:     name,
			Kind:     KindNative,
			Blurb:    p.Prompt(),
			Writable: p.Writable(),
		})
	}
	return out
}
