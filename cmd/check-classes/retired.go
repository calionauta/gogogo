// SCOPE:core - DO NOT REMOVE - classes DaisyUI 4 shipped and 5 does not.
//
// A retired-class table is not a second copy of DaisyUI's vocabulary (the skill
// is that): it is the migration fact the skill cannot carry, because the skill
// describes v5 and says nothing about v4 names. Without it a v4 relic renders as
// nothing and every other gate stays green — `label-text` appeared 18 times and
// `form-control` 17 times in this repo, both unstyled.
//
// Keep this list tiny and justified: one entry per class that a v4→v5 migration
// actually leaves behind. `replacement` is printed in the failure so the fix is
// the message, not a doc lookup.
package main

// retiredClass is a class DaisyUI 4 shipped, with what to use in 5.
type retiredClass struct {
	when        string // the v5 shape that replaces it
	replacement string // the class/pattern to write instead
}

// retiredClasses is the v4 surface this repo has used or could plausibly carry.
// It is intentionally short: the migration removed variant suffixes and the
// form/label helper classes, not components.
var retiredClasses = map[string]retiredClass{
	"label-text":        {"the text inside a label", "`label` on a <span>, or plain text (v5 has no label-text)"},
	"label-text-alt":    {"secondary label text", "plain text with a text-xs utility"},
	"form-control":      {"a field wrapped by a label", "`label` (the element) + `input`; v5 has no form-control"},
	"input-bordered":    {"a bordered input", "`input` (v5 inputs are bordered by default)"},
	"textarea-bordered": {"a bordered textarea", "`textarea` (bordered by default in v5)"},
	"select-bordered":   {"a bordered select", "`select` (bordered by default in v5)"},
	"tab-sm":            {"a small tab", "`tabs-sm` on the `tabs` container, not on each `tab`"},
	"tab-md":            {"a medium tab", "`tabs-md` on the `tabs` container"},
	"tab-lg":            {"a large tab", "`tabs-lg` on the `tabs` container"},
	"card-compact":      {"a compact card", "`card` + padding utilities (v5 dropped card-compact)"},
	"card-bordered":     {"a bordered card", "`card border border-base-300`"},
}
