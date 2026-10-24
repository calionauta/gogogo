// SCOPE:core — Vendored SVG path catalog for the Icon component.
//
// The bodies are Material Symbols glyphs vendored from Iconify
// (api.iconify.design/material-symbols/{name}.svg) at the time of the
// iconify removal. One owner on purpose: before this, ten
// <iconify-icon> call sites depended on a runtime web component
// (/static/iconify-icon.min.js) that lays out at 0x0 without its script,
// needed a syncIcons() workaround in theme.js, and loaded twice per page
// (ThemeHead AND the app assets). Inline SVG has no script, no flash,
// and inherits the surrounding font size via width/height 1em.
//
// Keys keep the full "material-symbols:..." names so internal/ui/modules.go
// needs no translation layer (its Icon field feeds Icon() directly) and a
// trim still drops module lines without touching this file.
//
// Adding a glyph: vendor the body from the URL above (keep ONLY the inner
// <path>, the <svg> wrapper lives in icons.templ), add the map entry, and
// extend TestIconCatalogCoversModules if it backs a module.
package icon

// iconPath returns the vendored inner SVG for a Material Symbols glyph.
// An unknown name returns "": the <svg> renders empty (visible as missing
// in review captures), and the coverage tests pin every name the tree uses,
// so a typo fails loudly in CI instead of silently on the page.
func iconPath(name string) string {
	return iconPaths[name]
}

var iconPaths = map[string]string{
	"material-symbols:checklist":      `<path fill="currentColor" d="M5.55 19L2 15.45l1.4-1.4l2.125 2.125l4.25-4.25l1.4 1.425zm0-8L2 7.45l1.4-1.4l2.125 2.125l4.25-4.25l1.4 1.425zM13 17v-2h9v2zm0-8V7h9v2z"/>`,
	"material-symbols:draw":           `<path fill="currentColor" d="M4 21v-4.25L17.175 3.6q.3-.3.675-.45T18.6 3q.4 0 .763.15T20 3.6L21.4 5q.3.275.45.638T22 6.4q0 .375-.15.75t-.45.675L8.25 21zM18.65 7.775L20 6.425L18.575 5l-1.35 1.35zM14 21q1.85 0 3.425-.925T19 17.5q0-.9-.475-1.55t-1.275-1.125L15.775 16.3q.575.25.9.55t.325.65q0 .575-.913 1.038T14 19q-.425 0-.712.288T13 20t.288.713T14 21m-9.425-7.65l1.5-1.5q-.5-.2-.788-.412T5 11q0-.3.45-.6t1.9-.925q2.2-.95 2.925-1.725T11 6q0-1.375-1.1-2.187T7 3q-1.125 0-2.013.4t-1.362.975Q3.35 4.7 3.4 5.1t.375.65q.325.275.725.225t.675-.325q.35-.35.775-.5T7 5q1.025 0 1.513.3T9 6q0 .35-.437.637T6.55 7.65q-2 .875-2.775 1.588T3 11q0 .8.425 1.363t1.15.987"/>`,
	"material-symbols:description":    `<path fill="currentColor" d="M8 18h8v-2H8zm0-4h8v-2H8zm-2 8q-.825 0-1.412-.587T4 20V4q0-.825.588-1.412T6 2h8l6 6v12q0 .825-.587 1.413T18 22zm7-13h5l-5-5z"/>`,
	"material-symbols:auto-awesome":   `<path fill="currentColor" d="m19 9l-1.25-2.75L15 5l2.75-1.25L19 1l1.25 2.75L23 5l-2.75 1.25L19 9Zm0 14l-1.25-2.75L15 19l2.75-1.25L19 15l1.25 2.75L23 19l-2.75 1.25L19 23ZM9 20l-2.5-5.5L1 12l5.5-2.5L9 4l2.5 5.5L17 12l-5.5 2.5L9 20Z"/>`,
	"material-symbols:forum":          `<path fill="currentColor" d="M7 18q-.425 0-.712-.288T6 17v-2h13V6h2q.425 0 .713.288T22 7v15l-4-4zm-5-1V3q0-.425.288-.712T3 2h13q.425 0 .713.288T17 3v9q0 .425-.288.713T16 13H6z"/>`,
	"material-symbols:fact-check":     `<path fill="currentColor" d="M4 21q-.825 0-1.412-.587T2 19V5q0-.825.588-1.412T4 3h16q.825 0 1.413.588T22 5v14q0 .825-.587 1.413T20 21zm1-4h5v-2H5zm9.55-2l4.95-4.95l-1.425-1.425l-3.525 3.55l-1.425-1.425l-1.4 1.425zM5 13h5v-2H5zm0-4h5V7H5z"/>`,
	"material-symbols:groups":         `<path fill="currentColor" d="M0 18v-1.575q0-1.075 1.1-1.75T4 14q.325 0 .625.013t.575.062q-.35.525-.525 1.1t-.175 1.2V18zm6 0v-1.625q0-.8.438-1.463t1.237-1.162T9.588 13T12 12.75q1.325 0 2.438.25t1.912.75t1.225 1.163t.425 1.462V18zm13.5 0v-1.625q0-.65-.162-1.225t-.488-1.075q.275-.05.563-.062T20 14q1.8 0 2.9.663t1.1 1.762V18zM4 13q-.825 0-1.412-.587T2 11q0-.85.588-1.425T4 9q.85 0 1.425.575T6 11q0 .825-.575 1.413T4 13m16 0q-.825 0-1.412-.587T18 11q0-.85.588-1.425T20 9q.85 0 1.425.575T22 11q0 .825-.575 1.413T20 13m-8-1q-1.25 0-2.125-.875T9 9q0-1.275.875-2.137T12 6q1.275 0 2.138.863T15 9q0 1.25-.862 2.125T12 12"/>`,
	"material-symbols:settings":       `<path fill="currentColor" d="m9.25 22l-.4-3.2q-.325-.125-.612-.3t-.563-.375L4.7 19.375l-2.75-4.75l2.575-1.95Q4.5 12.5 4.5 12.338v-.675q0-.163.025-.338L1.95 9.375l2.75-4.75l2.975 1.25q.275-.2.575-.375t.6-.3l.4-3.2h5.5l.4 3.2q.325.125.613.3t.562.375l2.975-1.25l2.75 4.75l-2.575 1.95q.025.175.025.338v.674q0 .163-.05.338l2.575 1.95l-2.75 4.75l-2.95-1.25q-.275.2-.575.375t-.6.3l-.4 3.2zm2.8-6.5q1.45 0 2.475-1.025T15.55 12t-1.025-2.475T12.05 8.5q-1.475 0-2.488 1.025T8.55 12t1.013 2.475T12.05 15.5"/>`,
	"material-symbols:menu":           `<path fill="currentColor" d="M3 18v-2h18v2zm0-5v-2h18v2zm0-5V6h18v2z"/>`,
	"material-symbols:layers":         `<path fill="currentColor" d="m12 21.05l-9-7l1.65-1.25L12 18.5l7.35-5.7L21 14.05zM12 16L3 9l9-7l9 7z"/>`,
	"material-symbols:dark-mode":      `<path fill="currentColor" d="M12 21q-3.75 0-6.375-2.625T3 12t2.625-6.375T12 3q.35 0 .688.025t.662.075q-1.025.725-1.638 1.888T11.1 7.5q0 2.25 1.575 3.825T16.5 12.9q1.375 0 2.525-.613T20.9 10.65q.05.325.075.662T21 12q0 3.75-2.625 6.375T12 21"/>`,
	"material-symbols:light-mode":     `<path fill="currentColor" d="M8.463 15.538Q7 14.075 7 12t1.463-3.537T12 7t3.538 1.463T17 12t-1.463 3.538T12 17t-3.537-1.463M5 13H1v-2h4zm18 0h-4v-2h4zM11 5V1h2v4zm0 18v-4h2v4zM6.4 7.75L3.875 5.325L5.3 3.85l2.4 2.5zm12.3 12.4l-2.425-2.525L17.6 16.25l2.525 2.425zM16.25 6.4l2.425-2.525L20.15 5.3l-2.5 2.4zM3.85 18.7l2.525-2.425L7.75 17.6l-2.425 2.525z"/>`,
	"material-symbols:volume-up":      `<path fill="currentColor" d="M14 20.725v-2.05q2.25-.65 3.625-2.5t1.375-4.2t-1.375-4.2T14 5.275v-2.05q3.1.7 5.05 3.138T21 11.975t-1.95 5.613T14 20.725M3 15V9h4l5-5v16l-5-5zm11 1V7.95q1.175.55 1.838 1.65T16.5 12q0 1.275-.663 2.363T14 16"/>`,
	"material-symbols:volume-off":     `<path fill="currentColor" d="m19.8 22.6l-3.025-3.025q-.625.4-1.325.688t-1.45.462v-2.05q.35-.125.688-.25t.637-.3L12 14.8V20l-5-5H3V9h3.2L1.4 4.2l1.4-1.4l18.4 18.4zm-.2-5.8l-1.45-1.45q.425-.775.638-1.625t.212-1.75q0-2.35-1.375-4.2T14 5.275v-2.05q3.1.7 5.05 3.138T21 11.975q0 1.325-.363 2.55T19.6 16.8m-3.35-3.35L14 11.2V7.95q1.175.55 1.838 1.65T16.5 12q0 .375-.062.738t-.188.712M12 9.2L9.4 6.6L12 4z"/>`,
	"material-symbols:delete-outline": `<path fill="currentColor" d="M7 21q-.825 0-1.412-.587T5 19V6H4V4h5V3h6v1h5v2h-1v13q0 .825-.587 1.413T17 21zM17 6H7v13h10zM9 17h2V8H9zm4 0h2V8h-2zM7 6v13z"/>`,
}
