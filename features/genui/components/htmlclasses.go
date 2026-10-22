// SCOPE:plugin — the class vocabulary model-authored markup may use.
package components

import (
	"slices"
	"strings"
)

// ModelHTMLClasses is the skin-agnostic half of the vocabulary for
// html_block markup: plain Tailwind layout, spacing, type and surface
// utilities. Every entry must exist under BOTH skins, so it is
// safelisted in src/css/input.css (DaisyUI) and
// src/css/input.css (the one bundle).
//
// Why a closed list at all: Tailwind emits only the classes it finds
// while scanning the tree, and model markup exists at RUNTIME — nothing
// in the repository contains the classes a live model invents. Measured
// on this tree before the safelist existed: `opacity-60` and
// `sm:grid-cols-3` (both used by the keyless chat demo answer) were
// absent from app.min.css, so an invented visual silently rendered
// unstyled. TestModelHTMLClassVocabulary fails when a class here is
// missing from a safelist or from a built bundle, and the failure names
// the fix.
var ModelHTMLClasses = []string{
	// structure
	"flex", "inline-flex", "grid", "block",
	"items-center", "items-start", "justify-between", "justify-center",
	"grid-cols-1", "grid-cols-2", "grid-cols-3", "grid-cols-4",
	"sm:grid-cols-2", "sm:grid-cols-3", "col-span-2",
	"gap-1", "gap-2", "gap-3", "gap-4",
	"w-full", "min-w-full", "max-w-md", "mx-auto", "overflow-x-auto",
	// spacing
	"p-2", "p-3", "p-4", "px-3", "px-4", "py-2", "py-3",
	"mt-1", "mt-2", "mt-3", "mt-4", "mb-1", "mb-2", "mb-3", "mb-4",
	// type
	"text-xs", "text-sm", "text-base", "text-lg", "text-xl", "text-2xl",
	"font-semibold", "font-bold", "uppercase", "tracking-wide",
	"leading-relaxed", "whitespace-pre-wrap",
	"opacity-60", "opacity-70",
	// surface
	"rounded", "rounded-lg", "border", "shadow", "shadow-md",
	"list-disc", "list-none", "text-left", "text-right", "truncate",
}

// ModelHTMLComponents is the themed half: the DaisyUI components and
// semantic colours the shipped skin provides. Themes change those tokens
// per palette (docs/ui-skins.md), so this half is safelisted in the
// bundle — which is exactly why the
// prompt asks the model to reach for a component here rather than
// invent a themed surface, and why the assistant bubble keeps its shape
// when the skin is switched.
var ModelHTMLComponents = []string{
	"card", "card-body", "card-title", "card-actions",
	"badge", "badge-primary", "badge-success", "badge-warning", "badge-error", "badge-outline",
	"alert", "alert-info", "alert-success", "alert-warning", "alert-error",
	"progress", "progress-primary", "progress-success",
	"table", "table-zebra", "table-sm",
	"bg-base-100", "bg-base-200", "bg-base-300",
	"text-base-content", "text-base-content/60", "text-primary", "text-error",
}

// ModelHTMLVocabulary is the list the html_block prompt shows the model:
// "use these and nothing else", because everything outside it renders
// unstyled. Ordered utilities first, themed components last.
func ModelHTMLVocabulary() string {
	return strings.Join(slices.Concat(ModelHTMLClasses, ModelHTMLComponents), " ")
}
