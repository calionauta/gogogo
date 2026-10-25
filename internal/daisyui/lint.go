// SCOPE:core — the DaisyUI conformance rules (the reusable half of the gate).
//
// Five rules derive from the vendored skill and one is a policy:
//
//	UNKNOWN_CLASS        a class that starts with a component the file already
//	                     uses must be declared by the skill for it; the message
//	                     names the nearest declared class ("message = the fix").
//	PART_WITHOUT_PARENT  a part (`- part:` in the skill) needs its component in
//	                     the same file.
//	HOST_MISMATCH        a class the skill documents on exactly one form element
//	                     belongs there (a `toggle` on a <button> holds no state).
//	RETIRED_CLASS        a DaisyUI 4 name, with its v5 replacement in the message.
//	DUPLICATE_MODIFIER   one color and one size per element (the skill's own
//	                     "{MODIFIER}" rule); two sizes is a coin-flip in review.
//	ARBITRARY_VALUE      a `[13px]`-style value outside the theme scale: warning
//	                     by default, error with Strict, allowlisted by token.
//
// Nothing here is hand-maintained vocabulary: everything but the retirement table
// and the arbitrary-value policy comes from parsing the skill, so a DaisyUI
// version bump widens (or narrows) the checks mechanically.
//
// Stable codes matter because a finding is read by an agent that has to decide
// what to do: `UNKNOWN_CLASS` is a correction, `ARBITRARY_VALUE` is a judgement
// call, `RETIRED_CLASS` is a migration. The codes are part of the contract with
// cmd/daisyui-lint's users and should only be added, never renamed.
package daisyui

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// Severity is how hard a finding fails the run.
type Severity int

const (
	// Warning reports without failing unless Options.Strict is set.
	Warning Severity = iota
	// Error always fails.
	Error
)

func (s Severity) String() string {
	if s == Error {
		return "error"
	}
	return "warning"
}

// Code is a stable diagnostic identifier.
type Code string

const (
	CodeUnknownClass      Code = "UNKNOWN_CLASS"
	CodePartWithoutParent Code = "PART_WITHOUT_PARENT"
	CodeHostMismatch      Code = "HOST_MISMATCH"
	CodeRetiredClass      Code = "RETIRED_CLASS"
	CodeDuplicateModifier Code = "DUPLICATE_MODIFIER"
	CodeArbitraryValue    Code = "ARBITRARY_VALUE"
)

// Finding is one broken rule.
type Finding struct {
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

// String renders the one-line form agents read: file:line: SEVERITY CODE: message.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s %s: %s", f.File, f.Line, f.Severity, f.Code, f.Message)
}

// Options is the policy the caller owns.
type Options struct {
	// Strict promotes warnings (arbitrary values) to errors.
	Strict bool
	// Allow lists arbitrary-value tokens a project has accepted, e.g.
	// "min-h-[7rem]". Exact match on the class token.
	Allow []string
	// RepoOwned is the set of class names the CALLER styles itself (its own kit
	// or hand-written stylesheets). DaisyUI's namespace yields to it, so a local
	// `chat-layout` is never judged against DaisyUI's `chat`.
	RepoOwned map[string]bool
}

func (o Options) allows(token string) bool {
	return slices.Contains(o.Allow, token)
}

func (o Options) failure() Severity {
	if o.Strict {
		return Error
	}
	return Warning
}

// LintFile applies every rule to one file's markup.
func LintFile(vocab Vocabulary, src, rel string, opts Options) []Finding {
	var out []Finding
	classes := ClassUses(src)

	present := map[string]bool{}
	for _, c := range classes {
		present[c.Name] = true
	}

	for _, c := range classes {
		out = append(out, retiredFinding(c, rel)...)
		if f, ok := arbitraryFinding(c, rel, opts); ok {
			out = append(out, f)
		}
		if opts.RepoOwned[c.Name] {
			continue
		}
		spec := vocab.owner(c.Name)
		if spec == nil {
			continue
		}
		out = append(out, spec.findings(c, rel, present)...)
	}
	out = append(out, duplicateModifierFindings(vocab, classes, rel)...)
	return out
}

// retiredFinding reports a DaisyUI 4 name with the v5 replacement.
func retiredFinding(c Class, rel string) []Finding {
	r, ok := RetiredClasses[c.Name]
	if !ok {
		return nil
	}
	return []Finding{{
		File: rel, Line: c.Line, Code: CodeRetiredClass, Severity: Error,
		Message: fmt.Sprintf("class %q is retired in DaisyUI 5 (%s): use %s", c.Name, r.When, r.Replacement),
	}}
}

// styleAxis lists the Tailwind axes where an arbitrary value means an off-scale
// design decision. A bracket value on any other axis (a grid template, a
// calc()) is legitimate, so it is not reported.
var styleAxis = regexp.MustCompile(
	`^(text|leading|tracking|indent|gap|gap-x|gap-y|p|px|py|pt|pb|pl|pr|ps|pe|m|mx|my|mt|mb|ml|mr|ms|me|` +
		`w|h|min-w|max-w|min-h|max-h|size|rounded|top|right|bottom|left|inset|z|border|shadow|opacity|space-[xy])-\[`)

// arbitraryFinding reports a bracket value on a style axis: the drift class that
// the 2026 provenance studies measure growing with AI involvement.
func arbitraryFinding(c Class, rel string, opts Options) (Finding, bool) {
	if !strings.Contains(c.Name, "[") || !styleAxis.MatchString(c.Name) || opts.allows(c.Name) {
		return Finding{}, false
	}
	return Finding{
		File: rel, Line: c.Line, Code: CodeArbitraryValue, Severity: opts.failure(),
		Message: fmt.Sprintf("class %q steps off the theme scale: use a scale value or Tailwind's "+
			"token, or allowlist the token in .daisyui-allow", c.Name),
	}, true
}

// findings applies the three per-component rules to one class use.
func (spec *Component) findings(c Class, rel string, present map[string]bool) []Finding {
	var out []Finding
	switch {
	case spec.Parts[c.Name] && !present[spec.Base]:
		out = append(out, Finding{
			File: rel, Line: c.Line, Code: CodePartWithoutParent, Severity: Error,
			Message: fmt.Sprintf("daisyUI part %q needs its %q container in the same file", c.Name, spec.Base),
		})
	case !spec.Classes[c.Name] && present[spec.Base]:
		out = append(out, Finding{
			File: rel, Line: c.Line, Code: CodeUnknownClass, Severity: Error,
			Message: unknownClassMessage(spec, c.Name),
		})
	}
	if host, ok := spec.singleFormHost(); ok && c.Tag != "" && c.Tag != host {
		out = append(out, Finding{
			File: rel, Line: c.Line, Code: CodeHostMismatch, Severity: Error,
			Message: fmt.Sprintf("daisyUI %q belongs on an <%s> (native state), found <%s>", spec.Base, host, c.Tag),
		})
	}
	return out
}

// unknownClassMessage names the nearest class the skill declares, so the failure
// is also the fix. A suggestion is offered only when it is close.
func unknownClassMessage(spec *Component, got string) string {
	msg := fmt.Sprintf("daisyUI %q is not a %s class the vendored skill declares", got, spec.Base)
	if near := spec.nearest(got); near != "" {
		msg += fmt.Sprintf("; did you mean %q?", near)
	} else {
		msg += " (typo, or the skill is stale: refresh it from daisyui.com/llms.txt)"
	}
	return msg
}

// nearest returns the declared class within edit distance 2, if any.
func (spec *Component) nearest(got string) string {
	best, bestDist := "", 3
	names := make([]string, 0, len(spec.Classes))
	for name := range spec.Classes {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic suggestion
	for _, name := range names {
		if d := editDistance(got, name); d < bestDist {
			best, bestDist = name, d
		}
	}
	return best
}

// duplicateModifierFindings reports two sizes or two colors on one element: the
// skill's "{MODIFIER}" is one color plus one size, and two of either is a
// coin-flip that renders the last one in source order.
func duplicateModifierFindings(vocab Vocabulary, classes []Class, rel string) []Finding {
	var out []Finding
	seen := map[string]map[string]bool{} // family|line|elem -> class
	for _, c := range classes {
		spec := vocab.owner(c.Name)
		if spec == nil {
			continue
		}
		family := ""
		switch {
		case spec.Sizes[c.Name]:
			family = spec.Base + "-size"
		case spec.Colors[c.Name]:
			family = spec.Base + "-color"
		default:
			continue
		}
		key := fmt.Sprintf("%s|%d|%d|%d", family, c.Line, c.Elem, c.Lit)
		if seen[key] == nil {
			seen[key] = map[string]bool{}
		}
		seen[key][c.Name] = true
		if len(seen[key]) == 2 {
			// Report once per element, when the second one appears.
			out = append(out, Finding{
				File: rel, Line: c.Line, Code: CodeDuplicateModifier, Severity: Error,
				Message: fmt.Sprintf("two %s classes on one element (%s): keep one",
					family[len(spec.Base)+1:], strings.Join(sortedKeys(seen[key]), ", ")),
			})
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// singleFormHost reports the one form element a component's class is documented
// on, when the skill agrees on exactly one.
func (spec *Component) singleFormHost() (string, bool) {
	if len(spec.Hosts) != 1 {
		return "", false
	}
	for host := range spec.Hosts {
		switch host {
		case "input", "select", "textarea":
			return host, true
		}
	}
	return "", false
}

// owner returns the component a class belongs to: the exact name first, then the
// longest base that is a `base-` prefix of the token.
func (v Vocabulary) owner(name string) *Component {
	if spec, ok := v[name]; ok {
		return spec
	}
	var best *Component
	for base, spec := range v {
		if !strings.HasPrefix(name, base+"-") {
			continue
		}
		if best == nil || len(base) > len(best.Base) {
			best = spec
		}
	}
	return best
}

// editDistance is Levenshtein, capped by the caller's threshold.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
