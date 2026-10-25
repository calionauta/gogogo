package daisyui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fillerComponents pads the fixture skill past minComponents. The real ones
// below are the shapes every rule is exercised on, and they mirror the vendored
// skill's formatting closely enough to keep the parser honest.
const fillerComponents = 41

func fence(html string) string { return "```html\n" + html + "\n```\n\n" }

func component(name, classNames, syntax string) string {
	return "### " + name + "\n\n#### Class names\n" + classNames + "\n#### Syntax\n" + fence(syntax)
}

func testSkill() string {
	var b strings.Builder
	b.WriteString("# daisyUI 5\n\n## daisyUI components\n\n")
	for i := range fillerComponents {
		name := fmt.Sprintf("filler%d", i)
		fmt.Fprintf(&b, "### Filler %d\n\n#### Class names\n- component: `%s`\n\n", i, name)
		fmt.Fprintf(&b, "#### Syntax\n%s", fence(`<div class="`+name+`"></div>`))
	}
	b.WriteString(component("Toggle",
		"- component: `toggle`\n- color: `toggle-primary`\n- size: `toggle-xs`, `toggle-sm`\n",
		`<input type="checkbox" class="toggle {MODIFIER}" />`))
	b.WriteString(component("Join", "- component: `join`\n- part: `join-item`\n",
		`<div class="join"><input class="join-item" /><button class="join-item"></button></div>`))
	b.WriteString(component("Dropdown", "- component: `dropdown`\n- part: `dropdown-content`\n",
		`<details class="dropdown"><summary></summary><ul class="dropdown-content"></ul></details>`))
	// `input` appears on two elements across the document (this section and the
	// Label one): the host rule must tolerate a class with two documented hosts.
	b.WriteString(component("Label", "- component: `label`\n",
		`<label class="input"><span class="label">{text}</span><input type="text" /></label>`))
	b.WriteString(component("Input", "- component: `input`\n- size: `input-sm`, `input-lg`\n",
		`<input type="text" class="input {MODIFIER}" />`))
	b.WriteString(component("Chat", "- component: `chat`\n- part: `chat-bubble`\n",
		`<div class="chat"><div class="chat-bubble"></div></div>`))
	return b.String()
}

func vocabularyOf(t *testing.T, skill string) Vocabulary {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(skill), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := LoadSkillFile(path)
	if err != nil {
		t.Fatalf("LoadSkill: %v", err)
	}
	return v
}

func veto(t *testing.T) Vocabulary { t.Helper(); return vocabularyOf(t, testSkill()) }

// lint runs the rules over one markup snippet (the file name is cosmetic).
func lint(t *testing.T, v Vocabulary, markup string, opts Options) []Finding {
	t.Helper()
	return LintFile(v, markup, "page.templ", opts)
}

func has(findings []Finding, code Code, substrings ...string) bool {
	for _, f := range findings {
		if f.Code != code {
			continue
		}
		ok := true
		for _, s := range substrings {
			if !strings.Contains(f.Message, s) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------- vocabulary

// A modifier that shares its component's prefix but is not declared is a typo.
// The message must name the nearest declared class: the failure is the fix.
func TestUnknownClassSuggestsNearest(t *testing.T) {
	t.Parallel()
	got := lint(t, veto(t), `<input type="checkbox" class="toggle toggle-smm"/>`, Options{})
	if !has(got, CodeUnknownClass, `"toggle-smm"`, `did you mean "toggle-sm"`) {
		t.Fatalf("expected the typo with a suggestion, got:\n%v", got)
	}
}

func TestDeclaredClassIsClean(t *testing.T) {
	t.Parallel()
	if got := lint(t, veto(t), `<input type="checkbox" class="toggle toggle-sm"/>`, Options{}); len(got) != 0 {
		t.Fatalf("expected clean, got:\n%v", got)
	}
}

// THE POINT OF DERIVING: adding the class to the skill widens the checks with
// no Go change. Same markup, two skills, two verdicts.
func TestSkillRefreshWidensCoverage(t *testing.T) {
	t.Parallel()
	markup := `<input type="checkbox" class="toggle toggle-sm"/>`
	without := strings.Replace(testSkill(), "- size: `toggle-xs`, `toggle-sm`\n", "- size: `toggle-xs`\n", 1)
	if got := lint(t, vocabularyOf(t, without), markup, Options{}); !has(got, CodeUnknownClass) {
		t.Fatalf("a class the skill does not declare must fail, got:\n%v", got)
	}
	if got := lint(t, veto(t), markup, Options{}); len(got) != 0 {
		t.Fatalf("the same markup must pass once the skill declares it, got:\n%v", got)
	}
}

// A component section the parser cannot read is an ERROR, not a silent skip: the
// auto-bump PR must break loudly instead of quietly narrowing the gate.
func TestSkillShapeTripwire(t *testing.T) {
	t.Parallel()
	broken := testSkill() + "\n### Mystery\n\n#### Class names\n\n- `something`\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSkillFile(path); err == nil || !strings.Contains(err.Error(), "Mystery") {
		t.Fatalf("expected the shape error to name the section, got: %v", err)
	}
}

func TestSkillFloorTripwire(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	one := "### Only\n\n#### Class names\n- component: `only`\n\n#### Syntax\n" +
		fence(`<div class="only"></div>`)
	if err := os.WriteFile(path, []byte(one), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSkillFile(path); err == nil || !strings.Contains(err.Error(), "expected at least") {
		t.Fatalf("expected the component-count tripwire, got: %v", err)
	}
}

// ---------------------------------------------------------------- parts

func TestPartNeedsItsContainer(t *testing.T) {
	t.Parallel()
	v := veto(t)
	got := lint(t, v, `<input class="input join-item"/>`, Options{})
	if !has(got, CodePartWithoutParent, `"join-item"`, `"join"`) {
		t.Fatalf("expected the orphan part to fail, got:\n%v", got)
	}
	if got := lint(t, v, `<div class="join"><input class="input join-item"/></div>`, Options{}); len(got) != 0 {
		t.Fatalf("expected the part with its container to pass, got:\n%v", got)
	}
}

// ---------------------------------------------------------------- hosts

// Host policing applies only where the skill documents ONE form host: `toggle`
// on a button replaces native state, while `input` on a label is documented by
// the Label section and must not fire.
func TestHostMismatch(t *testing.T) {
	t.Parallel()
	v := veto(t)
	got := lint(t, v, `<button type="button" class="toggle"></button>`, Options{})
	if !has(got, CodeHostMismatch, `"toggle"`, "<button>") {
		t.Fatalf("toggle on a button must fail, got:\n%v", got)
	}
	if got := lint(t, v, `<input type="checkbox" class="toggle"/>`, Options{}); len(got) != 0 {
		t.Fatalf("toggle on an input must pass, got:\n%v", got)
	}
	label := `<label class="input"><span class="label">Email</span><input class="input" type="text"/></label>`
	if got := lint(t, v, label, Options{}); len(got) != 0 {
		t.Fatalf("input on a label is documented and must pass, got:\n%v", got)
	}
}

// ------------------------------------------------------------- retirement

func TestRetiredClassNamesItsReplacement(t *testing.T) {
	t.Parallel()
	got := lint(t, veto(t), `<span class="label-text">Email</span>`, Options{})
	if !has(got, CodeRetiredClass, `"label-text"`, "use `label` on a <span>") {
		t.Fatalf("expected the retirement message with its replacement, got:\n%v", got)
	}
}

// The table must never contradict the skill: `btn-disabled` was once listed here
// and is still shipped for <a> elements. This test derives the truth from the
// same file the gate reads, so a skill bump that re-adds a class fails here.
func TestRetiredTableDoesNotContradictTheSkill(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	vocab, err := LoadSkill(root)
	if err != nil {
		t.Skipf("no vendored skill in this tree: %v", err)
	}
	declared := map[string]bool{}
	for _, spec := range vocab {
		for class := range spec.Classes {
			declared[class] = true
		}
	}
	for name, r := range RetiredClasses {
		if declared[name] {
			t.Errorf("%q is listed as retired but the skill declares it", name)
		}
		if r.When == "" || r.Replacement == "" {
			t.Errorf("%q must say when it was used and what replaces it", name)
		}
	}
}

// ---------------------------------------------------------- modifier rules

// The skill's "{MODIFIER}" is one color plus one size. Two in ONE literal is an
// author error; two in different conditional branches is a legitimate pattern
// (the toast's error/success map), so the rule must stop at the literal.
func TestDuplicateModifierOnlyWithinOneLiteral(t *testing.T) {
	t.Parallel()
	v := veto(t)
	static := `<input type="checkbox" class="toggle toggle-sm toggle-xs"/>`
	if got := lint(t, v, static, Options{}); !has(got, CodeDuplicateModifier, "toggle-sm", "toggle-xs") {
		t.Fatalf("two sizes in one class string must fail, got:\n%v", got)
	}
	dynamic := `<div class={ templ.Classes("alert", ` +
		`map[string]bool{"alert-error": bad, "alert-success": ok}) }></div>`
	if got := lint(t, v, dynamic, Options{}); len(got) != 0 {
		t.Fatalf("conditional branches are not a duplicate, got:\n%v", got)
	}
}

// ------------------------------------------------------------ drift policy

// An off-scale value on a style axis is a warning by default and an error under
// --strict; a token the project has accepted is not reported at all.
func TestArbitraryValueSeverityAndAllowlist(t *testing.T) {
	t.Parallel()
	v := veto(t)
	markup := `<p class="text-[13px]">x</p>`

	relaxed := lint(t, v, markup, Options{})
	if len(relaxed) != 1 || relaxed[0].Code != CodeArbitraryValue || relaxed[0].Severity != Warning {
		t.Fatalf("expected one warning, got:\n%v", relaxed)
	}
	strict := lint(t, v, markup, Options{Strict: true})
	if len(strict) != 1 || strict[0].Severity != Error {
		t.Fatalf("--strict must promote it to an error, got:\n%v", strict)
	}
	allowed := lint(t, v, markup, Options{Strict: true, Allow: []string{"text-[13px]"}})
	if len(allowed) != 0 {
		t.Fatalf("an allowlisted token must be silent, got:\n%v", allowed)
	}
}

// A bracket value is only drift on a style axis: a grid template or a transform
// scale is a legitimate arbitrary value, and flagging it would teach people to
// ignore the rule.
func TestArbitraryValueIgnoresNonStyleAxes(t *testing.T) {
	t.Parallel()
	markup := `<div class="grid-cols-[1fr_2fr] scale-[1.02]"></div>`
	if got := lint(t, veto(t), markup, Options{Strict: true}); len(got) != 0 {
		t.Fatalf("non-style arbitrary values must not be reported, got:\n%v", got)
	}
}

// ----------------------------------------------------------------- policy

// A repo class that shares a DaisyUI prefix is the repo's vocabulary: the caller
// passes it as RepoOwned and DaisyUI's namespace yields.
func TestRepoOwnedPrefixWins(t *testing.T) {
	t.Parallel()
	markup := `<div class="chat-layout chat"></div>`
	if got := lint(t, veto(t), markup, Options{}); !has(got, CodeUnknownClass, "chat-layout") {
		t.Fatalf("without RepoOwned the ambiguity is reported, got:\n%v", got)
	}
	owned := lint(t, veto(t), markup, Options{RepoOwned: map[string]bool{"chat-layout": true}})
	if len(owned) != 0 {
		t.Fatalf("a repo-owned class must not be judged by DaisyUI, got:\n%v", owned)
	}
}

// ------------------------------------------------------------- discovery

// The standalone binary must find the skill from anywhere inside a project.
func TestFindSkillWalksUp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills/daisyui"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(root, "skills/daisyui/SKILL.md")
	if err := os.WriteFile(skill, []byte(testSkill()), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "web", "pages")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindSkill(nested)
	if err != nil || got != skill {
		t.Fatalf("FindSkill(%q) = %q, %v; want %q", nested, got, err, skill)
	}
}

func TestLoadAllowSkipsCommentsAndBlanks(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".daisyui-allow")
	body := "# accepted drift\n\ntext-[13px]\n  # indented comment\nmin-h-[7rem]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAllow(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "text-[13px]" || got[1] != "min-h-[7rem]" {
		t.Fatalf("unexpected allowlist: %v", got)
	}
}
