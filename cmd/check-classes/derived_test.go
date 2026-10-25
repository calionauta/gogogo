package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/uiregistry"
)

// ---------------------------------------------------------------- vocabulary

// RED-proof for the derived vocabulary: a modifier that shares its component's
// prefix but is not in the skill is a typo (or a stale skill), and the failure
// must say so instead of silently rendering nothing.
func TestDerivedVocabularyCatchesTypo(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor(),
		"features/x/components/page.templ": `<button class="toggle toggle-smm">x</button>`,
	})
	got := findingsFor(t, root)
	if !strings.Contains(got, `daisyUI "toggle-smm" is not a toggle class the vendored skill declares`) {
		t.Fatalf("expected the unknown modifier to fail, got:\n%s", got)
	}
}

// GREEN: the declared modifier is accepted, so the rule is not "any suffix".
func TestDerivedVocabularyAcceptsDeclared(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor(),
		"features/x/components/page.templ": `<input type="checkbox" class="toggle toggle-sm"/>`,
	})
	if got := findingsFor(t, root); got != "" {
		t.Fatalf("expected a clean tree, got:\n%s", got)
	}
}

// THE POINT OF DERIVING: adding a class to the skill widens the gate with no Go
// change. The same markup fails before the skill declares it and passes after.
func TestSkillRefreshWidensCoverage(t *testing.T) {
	t.Parallel()
	without := strings.Replace(fixtureSkill(),
		"- size: `toggle-xs`, `toggle-sm`\n", "- size: `toggle-xs`\n", 1)
	markup := `<input type="checkbox" class="toggle toggle-sm"/>`

	before := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor(),
		"features/x/components/page.templ": markup,
		".agents/skills/daisyui/SKILL.md":  without,
	})
	got := findingsFor(t, before)
	if !strings.Contains(got, `"toggle-sm" is not a toggle class`) {
		t.Fatalf("a class the skill does not declare must fail, got:\n%s", got)
	}

	after := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor(),
		"features/x/components/page.templ": markup,
	})
	if got := findingsFor(t, after); got != "" {
		t.Fatalf("the same markup must pass once the skill declares the class, got:\n%s", got)
	}
}

// RED-proof for the bump tripwire: a component section whose class names cannot
// be read must stop the gate instead of silently narrowing it.
func TestSkillShapeTripwire(t *testing.T) {
	t.Parallel()
	broken := fixtureSkill() + "\n### Mystery\n\n#### Class names\n\n- `something`\n"
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor(),
		"features/x/components/page.templ": `<div></div>`,
		".agents/skills/daisyui/SKILL.md":  broken,
	})
	_, err := checkTree(root)
	if err == nil || !strings.Contains(err.Error(), "Mystery") {
		t.Fatalf("expected the skill-shape error to name the section, got: %v", err)
	}
}

// A skill that parses but yields almost nothing is the silent-narrowing case.
func TestSkillFloorTripwire(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("### Only\n\n#### Class names\n- component: `only`\n\n")
	b.WriteString("#### Syntax\n```html\n<div class=\"only\"></div>\n```\n")
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor(),
		"features/x/components/page.templ": `<div></div>`,
		".agents/skills/daisyui/SKILL.md":  b.String(),
	})
	_, err := checkTree(root)
	if err == nil || !strings.Contains(err.Error(), "expected at least") {
		t.Fatalf("expected the component-count tripwire, got: %v", err)
	}
}

// A repo class that shares a DaisyUI prefix is this repo's vocabulary: chrome.css
// wins, so `chat-layout` is never judged against DaisyUI's `chat`.
func TestRepoOwnedPrefixWins(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("chat-layout"),
		"features/x/components/page.templ": `<div class="chat-layout"></div>`,
		".agents/skills/daisyui/SKILL.md": fixtureSkill() + "\n" + component("Chat", "Bubbles.",
			"- component: `chat`\n- part: `chat-bubble`\n", `<div class="chat"><div class="chat-bubble"></div></div>`),
	})
	if got := findingsFor(t, root); got != "" {
		t.Fatalf("a repo-owned prefix must not be judged by DaisyUI, got:\n%s", got)
	}
}

// ---------------------------------------------------------------- retirement

// RED-proof for the v4→v5 table: the failure must name the replacement, because
// the fix is the message.
func TestRetiredClassSpellsReplacement(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor(),
		"features/x/components/page.templ": `<span class="label-text">Email</span>`,
	})
	got := findingsFor(t, root)
	if !strings.Contains(got, `"label-text" is retired in DaisyUI 5`) ||
		!strings.Contains(got, "use `label` on a <span>") {
		t.Fatalf("expected the retirement message with its replacement, got:\n%s", got)
	}
}

// ---------------------------------------------------------------- host rule

// Host policing applies only where the skill documents ONE form host: `toggle`
// on a button replaces native state, while `input` on a label is documented by
// the Label section and must not fire.
func TestHostNeedsOneDocumentedHost(t *testing.T) {
	t.Parallel()
	onButton := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor(),
		"features/x/components/page.templ": `<button type="button" class="toggle"></button>`,
	})
	if got := findingsFor(t, onButton); !strings.Contains(got, `daisyUI "toggle" belongs on an <input>`) {
		t.Fatalf("toggle on a button must fail, got:\n%s", got)
	}

	onLabel := writeTree(t, map[string]string{
		"web/resources/static/chrome.css": cssFor(),
		"features/x/components/page.templ": `<label class="input"><span class="label">Email</span>` +
			`<input class="input" type="text"/></label>`,
	})
	if got := findingsFor(t, onLabel); got != "" {
		t.Fatalf("input on a label is documented (Label section) and must pass, got:\n%s", got)
	}
}

// ---------------------------------------------------------------- registry

// registryTree builds a fixture with one real source file and one entry.
func registryTree(t *testing.T, entry string, doc string) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("ui-empty"),
		"features/x/components/page.templ": `<div class="ui-empty"></div>`,
		"docs/ui-compositions.md":          doc,
		"internal/ui/registry.json":        `{"version":1,"entries":[` + entry + `]}`,
	})
}

const goodEntry = `{
  "id":"empty-state","title":"Empty state","when":"a list is empty","avoid":"ad-hoc margins",
  "source":"features/x/components/page.templ","shows":["ui-empty"],
  "doc":"docs/ui-compositions.md#1-empty-state"}`

const goodDoc = "# UI compositions\n\n## 1. Empty state\n\nCopy it.\n"

// GREEN: a registry entry that points at real markup and a real anchor passes.
func TestRegistryGoodEntryIsClean(t *testing.T) {
	t.Parallel()
	if got := findingsFor(t, registryTree(t, goodEntry, goodDoc)); got != "" {
		t.Fatalf("expected a clean registry, got:\n%s", got)
	}
}

// RED-proof: the entry claims a class the source no longer contains.
func TestRegistryEntryMustPointAtRealMarkup(t *testing.T) {
	t.Parallel()
	entry := strings.Replace(goodEntry, `"shows":["ui-empty"]`, `"shows":["ui-empty","ui-phantom"]`, 1)
	got := findingsFor(t, registryTree(t, entry, goodDoc))
	if !strings.Contains(got, `"ui-phantom", which no class attribute`) {
		t.Fatalf("expected the phantom class to fail, got:\n%s", got)
	}
}

// RED-proof: the anchor moved (a renamed composition section).
func TestRegistryDocAnchorMustResolve(t *testing.T) {
	t.Parallel()
	got := findingsFor(t, registryTree(t, goodEntry, "# UI compositions\n\n## 1. Emptiness\n"))
	if !strings.Contains(got, "anchor #1-empty-state does not resolve") {
		t.Fatalf("expected the dangling anchor to fail, got:\n%s", got)
	}
}

// RED-proof, both directions: a composition the doc numbers but the registry
// never lists.
func TestRegistryCoversEveryComposition(t *testing.T) {
	t.Parallel()
	doc := goodDoc + "\n## 2. Field plus its action\n\nJoin them.\n"
	got := findingsFor(t, registryTree(t, goodEntry, doc))
	if !strings.Contains(got, "composition #2-field-plus-its-action") {
		t.Fatalf("expected the undocumented composition to fail, got:\n%s", got)
	}
}

// An entry missing its rule of use is not usable by an agent.
func TestRegistryEntryNeedsItsProse(t *testing.T) {
	t.Parallel()
	entry := strings.Replace(goodEntry, `"avoid":"ad-hoc margins",`, "", 1)
	if got := findingsFor(t, registryTree(t, entry, goodDoc)); !strings.Contains(got, `has no avoid`) {
		t.Fatalf("expected the missing field to fail, got:\n%s", got)
	}
}

// The real repo's registry must validate, otherwise the gate is decorative.
func TestRealRegistryValidates(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	reg, err := uiregistry.Load(root)
	if err != nil {
		// A trimmed tree removes the shell unit, and the registry with it; the
		// capability's own tests assert the removal, so this is not a failure.
		t.Skipf("no registry in this tree: %v", err)
	}
	if got := registryFindings(root, reg); len(got) > 0 {
		t.Fatalf("the committed registry does not validate:\n%v", got)
	}
}

// The retirement table must never contradict the skill. `btn-disabled` was
// listed here and is still declared by daisyUI 5 (for <a> elements, which cannot
// take the disabled attribute) — a false positive waiting for the first link
// button. This test derives the truth from the same file the gate reads.
func TestRetiredClassesAreNotDeclaredByTheSkill(t *testing.T) {
	t.Parallel()
	specs, err := loadSkill(filepath.Join("..", ".."))
	if err != nil {
		t.Skipf("no vendored skill in this tree: %v", err)
	}
	declared := map[string]bool{}
	for _, spec := range specs {
		for class := range spec.Classes {
			declared[class] = true
		}
	}
	for name, retired := range retiredClasses {
		if declared[name] {
			t.Errorf("%q is listed as retired but the skill declares it", name)
		}
		if retired.when == "" || retired.replacement == "" {
			t.Errorf("%q must say when it was used and what replaces it", name)
		}
	}
}

// A registry entry's class has to be in a CLASS ATTRIBUTE, not merely somewhere
// in the file: a class named in a comment is not the markup the entry promises.
func TestRegistryClassMustBeInAClassAttribute(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("ui-empty"),
		"features/x/components/page.templ": "// ui-phantom lives only in this comment\n<div class=\"ui-empty\"></div>",
		"docs/ui-compositions.md":          goodDoc,
		"internal/ui/registry.json": `{"version":1,"entries":[{
  "id":"empty-state","title":"Empty state","when":"a list is empty","avoid":"ad-hoc margins",
  "source":"features/x/components/page.templ","shows":["ui-empty","ui-phantom"],
  "doc":"docs/ui-compositions.md#1-empty-state"}]}`,
	})
	got := findingsFor(t, root)
	if !strings.Contains(got, `"ui-phantom", which no class attribute`) {
		t.Fatalf("expected the comment-only class to fail, got:\n%s", got)
	}
}

// The schema version is checked, so a future format cannot be read silently.
func TestRegistryVersionIsChecked(t *testing.T) {
	t.Parallel()
	root := writeTree(t, map[string]string{
		"web/resources/static/chrome.css":  cssFor("ui-empty"),
		"features/x/components/page.templ": `<div class="ui-empty"></div>`,
		"docs/ui-compositions.md":          goodDoc,
		"internal/ui/registry.json": `{"version":2,"entries":[{
  "id":"empty-state","title":"Empty state","when":"a list is empty","avoid":"ad-hoc margins",
  "source":"features/x/components/page.templ","shows":["ui-empty"],
  "doc":"docs/ui-compositions.md#1-empty-state"}]}`,
	})
	if got := findingsFor(t, root); !strings.Contains(got, "version is 2") {
		t.Fatalf("expected the version mismatch to fail, got:\n%s", got)
	}
}
