package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testSkill is a small, valid skill in the vendored format (41 fillers keep the
// loader's component-count tripwire satisfied).
func testSkill() string {
	var b strings.Builder
	b.WriteString("# daisyUI 5\n\n## daisyUI components\n\n")
	for i := range 41 {
		name := fmt.Sprintf("filler%d", i)
		fmt.Fprintf(&b, "### Filler %d\n\n#### Class names\n- component: `%s`\n\n", i, name)
		fmt.Fprintf(&b, "#### Syntax\n```html\n<div class=\"%s\"></div>\n```\n\n", name)
	}
	b.WriteString("### Chat\n\n#### Class names\n- component: `chat`\n\n")
	b.WriteString("#### Syntax\n```html\n<div class=\"chat\"></div>\n```\n\n")
	return b.String()
}

// fixture writes a project tree and returns its root plus the skill path.
func fixture(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	skill := filepath.Join(root, "skills", "daisyui", "SKILL.md")
	all := map[string]string{skill: testSkill()}
	for rel, body := range files {
		all[filepath.Join(root, filepath.FromSlash(rel))] = body
	}
	for path, body := range all {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root, skill
}

// runCLI invokes the command in-process and returns its exit code and stdout.
func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	code, out, _ := runCLIAll(t, args...)
	return code, out
}

func runCLIAll(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut strings.Builder
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// RED-proof through the real CLI: a DaisyUI 4 relic fails, and the fix clears it.
func TestCLIReportsRetiredClassAndFixes(t *testing.T) {
	root, skill := fixture(t, map[string]string{
		"web/page.templ": `<span class="label-text">Email</span>`,
	})
	code, out := runCLI(t, "--skill", skill, root)
	if code != 1 || !strings.Contains(out, "RETIRED_CLASS") || !strings.Contains(out, "use `label`") {
		t.Fatalf("expected a RETIRED_CLASS failure, got code=%d out=%s", code, out)
	}

	good, skill2 := fixture(t, map[string]string{"web/page.templ": `<label class="label">Email</label>`})
	if code, out := runCLI(t, "--skill", skill2, good); code != 0 {
		t.Fatalf("expected clean, got code=%d out=%s", code, out)
	}
}

// A project's own stylesheet can own a DaisyUI-looking prefix: --css is how the
// CLI learns that, and without it the ambiguity is reported (honest default).
func TestCLIUsesCSSForRepoOwnedClasses(t *testing.T) {
	root, skill := fixture(t, map[string]string{
		"web/page.templ": `<div class="chat-layout chat"></div>`,
		"web/app.css":    ".chat-layout { display: grid; }\n",
	})
	if code, _ := runCLI(t, "--skill", skill, root); code != 1 {
		t.Fatalf("without --css the ambiguous class must be reported, got code=%d", code)
	}
	code, out := runCLI(t, "--skill", skill, "--css", filepath.Join(root, "web", "app.css"), root)
	if code != 0 {
		t.Fatalf("with --css it must be clean, got code=%d out=%s", code, out)
	}
}

// --strict is the policy knob: an off-scale value warns by default and fails CI
// when the project says so; --allow accepts a specific token.
func TestCLIArbitraryValuePolicy(t *testing.T) {
	root, skill := fixture(t, map[string]string{"web/page.templ": `<p class="text-[13px]">x</p>`})

	code, out := runCLI(t, "--skill", skill, root)
	if code != 0 || !strings.Contains(out, "warning ARBITRARY_VALUE") {
		t.Fatalf("expected a warning and exit 0, got code=%d out=%s", code, out)
	}
	if code, _ := runCLI(t, "--skill", skill, "--strict", root); code != 1 {
		t.Fatalf("--strict must fail, got code=%d", code)
	}
	allow := filepath.Join(root, ".daisyui-allow")
	if err := os.WriteFile(allow, []byte("text-[13px]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := runCLI(t, "--skill", skill, "--strict", "--allow", allow, root); code != 0 {
		t.Fatalf("an allowlisted token must pass, got code=%d out=%s", code, out)
	}
}

// --format json must always be an array (an agent parses it; `null` is a trap).
func TestCLIJSONIsAlwaysAnArray(t *testing.T) {
	root, skill := fixture(t, map[string]string{"web/page.templ": `<div class="chat"></div>`})
	code, out := runCLI(t, "--skill", skill, "--format", "json", root)
	if code != 0 {
		t.Fatalf("expected clean, got code=%d out=%s", code, out)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("expected an empty JSON array, got %q", out)
	}
}

// Fixtures and vendored trees are never linted: testdata intentionally breaks
// the rules, and a green run over it would be a lie in the other direction.
func TestCollectFilesSkipsFixtures(t *testing.T) {
	root, _ := fixture(t, map[string]string{
		"web/page.templ":            `<div class="chat"></div>`,
		"web/testdata/bad.html":     `<span class="label-text">x</span>`,
		"node_modules/pkg/bad.html": `<span class="label-text">x</span>`,
	})
	files, err := collectFiles([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f, "testdata") || strings.Contains(f, "node_modules") {
			t.Fatalf("fixture file was collected: %s", f)
		}
	}
	if len(files) != 1 {
		t.Fatalf("expected only the page, got %v", files)
	}
}

// A project keeps .daisyui-allow at its root; the linter finds it from anywhere
// inside the tree, like it finds the skill.
func TestCLIFindsAllowFileUpward(t *testing.T) {
	root, skill := fixture(t, map[string]string{
		"web/page.templ": `<p class="text-[13px]">x</p>`,
		".daisyui-allow": "text-[13px]\n",
	})
	nested := filepath.Join(root, "web")
	code, out := runCLI(t, "--skill", skill, "--strict", nested)
	if code != 0 {
		t.Fatalf("the allowlist at the root must be found from a subdirectory, got code=%d out=%s", code, out)
	}
}

// A glob that matches nothing is a silent loss of the project's vocabulary: the
// CLI says so instead of quietly judging local classes as DaisyUI's.
func TestCLINotesUnmatchedCSSGlob(t *testing.T) {
	root, skill := fixture(t, map[string]string{"web/page.templ": `<div class="chat"></div>`})
	code, _, errOut := runCLIAll(t, "--skill", skill, "--css", filepath.Join(root, "nope/*.css"), root)
	if code != 0 {
		t.Fatalf("expected clean, got code=%d", code)
	}
	if !strings.Contains(errOut, "matched no file") {
		t.Fatalf("expected a note about the empty glob, got %q", errOut)
	}
}

// A project with NO vendored skill is the normal case for a user who does not
// use gogogo: the binary must still work, say which vocabulary it used, and
// actually enforce it.
func TestCLIFallsBackToBundledVocabulary(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "index.html")

	if err := os.WriteFile(page, []byte(`<input type="checkbox" class="toggle toggle-sm"/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLIAll(t, "--strict", root)
	if code != 0 {
		t.Fatalf("expected clean, got code=%d out=%s err=%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "using the bundled vocabulary") {
		t.Fatalf("the fallback must be announced, got %q", errOut)
	}

	// A class the bundled vocabulary declares is clean; one it does not must
	// fail, which proves the run really linted instead of skipping.
	if err := os.WriteFile(page, []byte(`<span class="label-text">x</span>`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runCLIAll(t, root)
	if code != 1 || !strings.Contains(out, "RETIRED_CLASS") {
		t.Fatalf("expected RETIRED_CLASS with exit 1, got code=%d out=%s", code, out)
	}
}
