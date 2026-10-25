// SCOPE:core - DO NOT REMOVE - DaisyUI vocabulary, derived from the vendored skill.
//
// The gate must not own a second copy of DaisyUI's vocabulary. Until this file
// existed, cmd/check-classes hardcoded three part rules (`toggle` on an input,
// `join-item` needs `join`, `dropdown-content` needs `dropdown`) that a human
// transcribed from the skill by hand. That is the failure mode the gate exists
// to prevent: a copy nobody re-reads. The skill is refreshed by
// .github/workflows/daisyui-auto-bump.yml from https://daisyui.com/llms.txt
// (byte-pinned in UPSTREAM_SHA), so deriving here means a version bump widens
// the checks instead of leaving them behind.
//
// What is derived, and what stays a judgement call:
//
//   - CLASSES: every class the skill declares per component (component, part,
//     modifier, color, size, placement, style). A token that starts with a
//     known component but is not in that set is a typo or a stale skill.
//   - PARTS: `- part: …` lines, so "part needs its container in the same file"
//     generalises to every component instead of the two we happened to hit.
//   - HOST: the element the skill's own syntax sample puts the component on.
//     Enforced ONLY when that host is a form control (input/select/textarea):
//     DaisyUI's classes are legitimately used on many elements (`btn` on <a>,
//     `dropdown` on <details> or on a popover <ul>), so blanket host policing
//     would be noise. The prose ("a toggle is a checkbox") is not a schema.
//
// If upstream ever reshapes a component section so this parser yields no
// component class, that is an ERROR, not a silent skip: the bump PR must break
// loudly rather than quietly narrow the gate.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const skillPath = ".agents/skills/daisyui/SKILL.md"

// minComponents is the floor the parser expects from the vendored skill. It is
// a tripwire, not a target: daisyUI 5.7 ships 67 component sections. A parse
// that suddenly yields a handful means the format moved.
const minComponents = 40

// componentSpec is one DaisyUI component as the skill declares it.
type componentSpec struct {
	Name    string          // the skill's heading, e.g. "Dropdown"
	Base    string          // the component class, e.g. "dropdown"
	Hosts   map[string]bool // every element the skill's syntax samples put this class on
	Classes map[string]bool // every class name the skill allows for this component
	Parts   map[string]bool // the subset that are parts of this component
}

// backtickRE pulls `class-name` out of a prose bullet.
var backtickRE = regexp.MustCompile("`([^`]+)`")

// classTokenRE is what a usable class name looks like inside a backtick span.
var classTokenRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// loadSkill parses the vendored skill into a component index, or explains why
// it could not.
func loadSkill(root string) (map[string]*componentSpec, error) {
	// #nosec G703 -- local dev tool; the path is a package constant under root.
	raw, err := os.ReadFile(filepath.Join(root, skillPath))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w (the gate derives DaisyUI's vocabulary from it)", skillPath, err)
	}
	specs, parseErrs := parseSkill(string(raw))
	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("skill structure changed:\n  %s", strings.Join(parseErrs, "\n  "))
	}
	if len(specs) < minComponents {
		return nil, fmt.Errorf("parsed only %d components from %s (expected at least %d): "+
			"the skill's format moved, so the gate would silently stop checking it",
			len(specs), skillPath, minComponents)
	}
	return specs, nil
}

// parseSkill walks the `### ` sections that carry a `#### Class names` block.
// Sections without one (CDN setup, themes, the discovery protocol) are not
// components and are skipped.
func parseSkill(md string) (map[string]*componentSpec, []string) {
	specs := map[string]*componentSpec{}
	var broken []string

	// Hosts come from EVERY syntax sample in the document, not just the
	// component's own section: DaisyUI legitimately uses some classes on more
	// than one element (the `input` class styles the `<input>` AND, per the
	// Label section, a `<label>` container), and a class with more than one
	// documented host is not something a gate should police.
	classHosts := collectClassHosts(md)

	for _, sec := range splitSections(md) {
		lines := strings.Split(sec.body, "\n")
		cn := block(lines, "#### Class names")
		if cn == nil {
			continue
		}
		base, classes, parts := parseClassNameBullets(cn)
		if len(base) == 0 {
			broken = append(broken, fmt.Sprintf("%q declares classes but no component class", sec.name))
			continue
		}
		for _, b := range base {
			specs[b] = &componentSpec{
				Name:    sec.name,
				Base:    b,
				Hosts:   classHosts[b],
				Classes: classes,
				Parts:   parts,
			}
		}
	}
	return specs, broken
}

type skillSection struct {
	name string
	body string
}

// splitSections cuts the document into `### ` sections (the component level).
func splitSections(md string) []skillSection {
	var out []skillSection
	var name string
	var body strings.Builder
	flush := func() {
		if name != "" {
			out = append(out, skillSection{name: name, body: body.String()})
		}
	}
	for line := range strings.SplitSeq(md, "\n") {
		switch {
		case strings.HasPrefix(line, "### "):
			flush()
			name = strings.TrimSpace(strings.TrimPrefix(line, "### "))
			body.Reset()
		case strings.HasPrefix(line, "## "), strings.HasPrefix(line, "# "):
			flush()
			name = ""
			body.Reset()
		default:
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	flush()
	return out
}

// block returns the lines of one `#### X` block, or nil when absent.
func block(lines []string, heading string) []string {
	for i, line := range lines {
		if strings.TrimSpace(line) != heading {
			continue
		}
		var out []string
		for _, l := range lines[i+1:] {
			if strings.HasPrefix(strings.TrimSpace(l), "#### ") {
				return out
			}
			out = append(out, l)
		}
		return out
	}
	return nil
}

// parseClassNameBullets reads the categories the skill uses. Two shapes exist:
// `- part: a, b` and, for Calendar, a bare `- component` followed by nested
// `- \`name (description)\“ items — both are handled so a new component using
// the nested form broadens coverage instead of breaking the gate.
func parseClassNameBullets(lines []string) ([]string, map[string]bool, map[string]bool) {
	classes := map[string]bool{}
	parts := map[string]bool{}
	var base []string
	category := ""

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || !strings.HasPrefix(line, "- ") {
			continue
		}
		item := strings.TrimPrefix(line, "- ")
		name, value, hasValue := strings.Cut(item, ":")
		switch {
		case hasValue:
			category = strings.TrimSpace(name)
		case !strings.Contains(item, "`"):
			// The nested form: a bare `- component` opens the category and the
			// names live in the indented bullets below it.
			category = strings.TrimSpace(item)
		default:
			// A bullet that carries names but no category keeps the previous one.
		}
		text := item // nested form: the tokens live in the bullet itself
		if hasValue {
			text = value
		}
		tokens := namesIn(text)
		for _, tok := range tokens {
			classes[tok] = true
			switch category {
			case "component":
				base = appendUnique(base, tok)
			case "part":
				parts[tok] = true
			}
		}
	}
	return base, classes, parts
}

// namesIn extracts usable class names from a bullet's text.
func namesIn(text string) []string {
	var out []string
	for _, m := range backtickRE.FindAllStringSubmatch(text, -1) {
		// "cally (for Cally web component)" -> the class is the first word.
		tok := strings.Fields(m[1])
		if len(tok) == 0 {
			continue
		}
		if classTokenRE.MatchString(tok[0]) {
			out = append(out, tok[0])
		}
	}
	return out
}

func appendUnique(list []string, v string) []string {
	if slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}

// collectClassHosts maps every class name to the elements the skill's own
// syntax samples put it on. A class with two documented hosts (the `input`
// container case) yields two entries, which is how the gate knows not to
// enforce a host at all.
func collectClassHosts(md string) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, sec := range splitSections(md) {
		for _, fence := range fences(sec.body) {
			for line := range strings.SplitSeq(fence, "\n") {
				tag, class := elementOf(line)
				if tag == "" || class == "" {
					continue
				}
				for tok := range strings.FieldsSeq(class) {
					if !classTokenRE.MatchString(tok) {
						continue
					}
					if out[tok] == nil {
						out[tok] = map[string]bool{}
					}
					out[tok][tag] = true
				}
			}
		}
	}
	return out
}

// fences returns every fenced code block in a section body.
func fences(body string) []string {
	var out []string
	var cur strings.Builder
	in := false
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if in {
				out = append(out, cur.String())
				cur.Reset()
			}
			in = !in
			continue
		}
		if in {
			cur.WriteString(line)
			cur.WriteByte('\n')
		}
	}
	return out
}

// elementOf reads `<tag ... class="...">` from one line of an HTML sample.
func elementOf(line string) (string, string) {
	_, rest, found := strings.Cut(line, "<")
	if !found {
		return "", ""
	}
	name := strings.Fields(rest)
	if len(name) == 0 {
		return "", ""
	}
	tag := strings.ToLower(strings.TrimSuffix(name[0], ">"))
	if i := strings.IndexAny(tag, " \t/"); i >= 0 {
		tag = tag[:i]
	}
	before, after, found := strings.Cut(rest, "class=")
	if !found || strings.HasSuffix(before, "data-") || strings.HasSuffix(before, "-") {
		return tag, ""
	}
	if !strings.HasPrefix(after, `"`) {
		return tag, ""
	}
	class, _, found := strings.Cut(after[1:], `"`)
	if !found {
		return tag, ""
	}
	return tag, class
}
