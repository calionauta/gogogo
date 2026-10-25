// SCOPE:core — classes DaisyUI 4 shipped and 5 does not.
//
// A retirement table is not a second copy of DaisyUI's vocabulary (the skill is
// that): it is the migration fact the skill cannot carry, because the skill
// describes v5 and says nothing about v4 names. Without it a v4 relic renders as
// nothing and every other gate stays green — `label-text` appeared 18 times and
// `form-control` 17 times in the gogogo template, both unstyled.
//
// `TestRetiredClassesAreNotDeclaredByTheSkill` derives this table's truth from
// the skill, so it can never contradict it: a class DaisyUI still ships
// (`btn-disabled`, for <a> elements) belongs here only by mistake.
package daisyui

// retiredClass is a class DaisyUI 4 shipped, with what to use in 5.
type Retired struct {
	When        string // the v5 shape that replaces it
	Replacement string // the class/pattern to write instead
}

// retiredClasses is the v4 surface this repo has used or could plausibly carry.
// It is intentionally short: the migration removed variant suffixes and the
// form/label helper classes, not components.
var RetiredClasses = map[string]Retired{
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
