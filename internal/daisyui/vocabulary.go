// SCOPE:core — DaisyUI vocabulary, derived from the vendored skill.
//
// This is the reusable half of the class gate, extracted so a project that does
// NOT use gogogo can consume it: `cmd/daisyui-lint` is a standalone binary over
// this package, and the gogogo gate (`cmd/check-classes`) uses the same code
// rather than keeping a second copy.
//
// The vocabulary is never hand-maintained. It is parsed from the vendored DaisyUI
// skill (`.agents/skills/daisyui/SKILL.md`, refreshed from daisyui.com/llms.txt
// and pinned by UPSTREAM_SHA), so a version bump widens the checks instead of
// leaving them behind — and a reshape of a component section is an ERROR, not a
// silent narrowing.
package daisyui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// DefaultSkillPath is where the gate looks first, relative to the repo root.
const DefaultSkillPath = ".agents/skills/daisyui/SKILL.md"

// skillPath is kept as the internal alias the loader uses.
const skillPath = DefaultSkillPath

// DefaultAllowFile is the project's accepted-arbitrary-values list.
const DefaultAllowFile = ".daisyui-allow"

// minComponents is the floor the parser expects from the vendored skill. It is
// a tripwire, not a target: daisyUI 5.7 ships 67 component sections. A parse
// that suddenly yields a handful means the format moved.
const minComponents = 40

// componentSpec is one DaisyUI component as the skill declares it.
type Component struct {
	Name    string          // the skill's heading, e.g. "Dropdown"
	Base    string          // the component class, e.g. "dropdown"
	Hosts   map[string]bool // every element the skill's syntax samples put this class on
	Classes map[string]bool // every class name the skill allows for this component
	Parts   map[string]bool // the subset that are parts of this component
	Sizes   map[string]bool // the subset declared under `- size:`
	Colors  map[string]bool // the subset declared under `- color:`
}

// Vocabulary is the parsed skill, keyed by component base class.
type Vocabulary map[string]*Component

// backtickRE pulls `class-name` out of a prose bullet.
var backtickRE = regexp.MustCompile("`([^`]+)`")

// classTokenRE is what a usable class name looks like inside a backtick span.
var classTokenRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// FindSkill walks up from start looking for the vendored skill, so a standalone
// binary run anywhere inside a project finds it without a flag. The two shapes
// are the repo layout (.agents/skills/… symlink) and the plain skills/ copy.
func FindSkill(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		for _, rel := range []string{DefaultSkillPath, "skills/daisyui/SKILL.md"} {
			candidate := filepath.Join(dir, rel)
			if st, serr := os.Stat(candidate); serr == nil && !st.IsDir() {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no daisyUI skill found above %s "+
				"(expected %s or skills/daisyui/SKILL.md)", start, DefaultSkillPath)
		}
		dir = parent
	}
}

// FindAllow walks up from start looking for `.daisyui-allow`, mirroring
// FindSkill: a project keeps the file at its root and runs the linter from
// anywhere inside it.
func FindAllow(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, DefaultAllowFile)
		if st, serr := os.Stat(candidate); serr == nil && !st.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// LoadAllow reads a plain-text allowlist of arbitrary-value tokens, one per
// line, `#` starting a comment. Plain text on purpose: a policy file should not
// add a YAML dependency to the gate.
func LoadAllow(path string) ([]string, error) {
	// #nosec G304 G703 -- local dev tool; the path comes from the developer's own flag.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

// LoadSkill parses the vendored skill into a component index, or explains why
// it could not.
func LoadSkill(root string) (Vocabulary, error) {
	return LoadSkillFile(filepath.Join(root, filepath.FromSlash(skillPath)))
}

// LoadSkillFile parses one skill file. The standalone CLI takes a path, so the
// loader has to accept one.
func LoadSkillFile(path string) (Vocabulary, error) {
	// #nosec G304 G703 -- local dev tool; the path comes from the caller's flag,
	// a repo root, or the upward search.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w (the gate derives DaisyUI's vocabulary from it)", path, err)
	}
	specs, parseErrs := parseSkill(string(raw))
	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("skill structure changed:\n  %s", strings.Join(parseErrs, "\n  "))
	}
	if len(specs) < minComponents {
		return nil, fmt.Errorf("parsed only %d components from %s (expected at least %d): "+
			"the skill's format moved, so the gate would silently stop checking it",
			len(specs), path, minComponents)
	}
	return specs, nil
}

// parseSkill walks the `### ` sections that carry a `#### Class names` block.
// Sections without one (CDN setup, themes, the discovery protocol) are not
// components and are skipped.
func parseSkill(md string) (Vocabulary, []string) {
	specs := Vocabulary{}
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
		base, classes, parts, sizes, colors := parseClassNameBullets(cn)
		if len(base) == 0 {
			broken = append(broken, fmt.Sprintf("%q declares classes but no component class", sec.name))
			continue
		}
		for _, b := range base {
			specs[b] = &Component{
				Name:    sec.name,
				Base:    b,
				Hosts:   classHosts[b],
				Classes: classes,
				Parts:   parts,
				Sizes:   sizes,
				Colors:  colors,
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
func parseClassNameBullets(lines []string) ([]string, map[string]bool, map[string]bool,
	map[string]bool, map[string]bool,
) {
	classes := map[string]bool{}
	parts := map[string]bool{}
	sizes := map[string]bool{}
	colors := map[string]bool{}
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
			case "size":
				sizes[tok] = true
			case "color":
				colors[tok] = true
			}
		}
	}
	return base, classes, parts, sizes, colors
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

// SkillVersion reads the `version:` the skill declares about the DaisyUI release
// it documents (frontmatter). It is what `daisyui-lint --version` prints, so a
// consumer can tell which vocabulary the run used.
var skillVersionRE = regexp.MustCompile(`(?m)^\s*version:\s*(\S+)\s*$`)

func SkillVersion(path string) string {
	// #nosec G304 G703 -- local dev tool; the path is the one being linted against.
	raw, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	if m := skillVersionRE.FindStringSubmatch(string(raw)); len(m) == 2 {
		return m[1]
	}
	return "unknown"
}
