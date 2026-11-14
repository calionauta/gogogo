package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAllFilesParseAndValidate is the library-wide proof: every embedded
// .prompt file passes the same contract the gate enforces, so a broken file
// fails `go test` before it fails a request.
func TestAllFilesParseAndValidate(t *testing.T) {
	t.Parallel()
	all, err := All()
	if err != nil {
		t.Fatalf("All() = %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no prompt files embedded")
	}
	ids := make(map[string]bool, len(all))
	for _, p := range all {
		if ids[p.ID] {
			t.Errorf("duplicate prompt id %q", p.ID)
		}
		ids[p.ID] = true
		if strings.TrimSpace(p.Body) == "" {
			t.Errorf("%s: empty body", p.ID)
		}
	}
}

// TestRenderSubstitutesDeclaredVariable pins the happy path the call sites use.
func TestRenderSubstitutesDeclaredVariable(t *testing.T) {
	t.Parallel()
	got, err := Render("panels/todo-list", map[string]string{"filters": "open|done"})
	if err != nil {
		t.Fatalf("Render() = %v", err)
	}
	if !strings.Contains(got, "todo_list {filter: open|done}") {
		t.Errorf("filters not substituted:\n%s", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("unrendered placeholder left in output:\n%s", got)
	}
}

// TestRenderRejectsMissingVariable proves a half-filled prompt is refused: the
// alternative is sending a model the literal "{{partial}}".
func TestRenderRejectsMissingVariable(t *testing.T) {
	t.Parallel()
	if _, err := Render("chat/suggest-todo", nil); err == nil {
		t.Fatal("Render() with no variables = nil error, want missing-variable error")
	}
}

// writePrompt writes one valid prompt file for id under dir.
func writePrompt(t *testing.T, dir, id, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(id+".prompt"))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	raw := "---\nname: " + id + "\ndescription: \"override fixture\"\nversion: 1\nvariables: []\n---\n" + body + "\n"
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// TestOverrideShadowsEmbedded proves the GEPA seam: with the override directory
// set, the file on disk is what the caller gets — no rebuild, no restart.
func TestOverrideShadowsEmbedded(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "panels/todo-list", "OVERRIDDEN body")
	t.Setenv(OverrideDirEnv, dir)

	p, err := Get("panels/todo-list")
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if p.Body != "OVERRIDDEN body" {
		t.Errorf("body = %q, want the override file's body", p.Body)
	}
}

// TestOverrideFallsBackToEmbedded proves an override tree may hold only the
// prompts it changes: everything else still comes from the embedded library.
func TestOverrideFallsBackToEmbedded(t *testing.T) {
	dir := t.TempDir()
	writePrompt(t, dir, "panels/notes-list", "notes override")
	t.Setenv(OverrideDirEnv, dir)

	p, err := Get("panels/todo-list")
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if !strings.Contains(p.Body, "todo_list") {
		t.Errorf("body = %q, want the embedded todo-list prompt", p.Body)
	}
}

// TestOverrideRefusesPathEscape pins the containment: an id that walks out of
// the override directory is an error, never a read of a file above it. The
// fixture is a *valid* file whose frontmatter name is the escaping id, so the
// name check cannot be what refuses it — only os.Root can.
func TestOverrideRefusesPathEscape(t *testing.T) {
	base := t.TempDir()
	const escaping = "../outside/evil"
	p := filepath.Join(base, "outside", "evil.prompt")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := "---\nname: " + escaping + "\ndescription: \"escape fixture\"\nversion: 1\nvariables: []\n---\nESCAPED body\n"
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	override := filepath.Join(base, "override")
	if err := os.MkdirAll(override, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv(OverrideDirEnv, override)

	pr, err := Get(escaping)
	if err == nil {
		t.Fatalf("Get() with an escaping id = %q, want an error", pr.Body)
	}
	if strings.Contains(pr.Body, "ESCAPED") {
		t.Errorf("body leaked a file outside the override directory: %q", pr.Body)
	}
}

// TestOverrideInvalidFileFails proves the override tree obeys the same contract
// as the embedded one: a candidate prompt that does not parse is refused at
// load, not sent to a model.
func TestOverrideInvalidFileFails(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "panels", "todo-list.prompt")
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("not a prompt file"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(OverrideDirEnv, dir)

	if _, err := Get("panels/todo-list"); err == nil {
		t.Fatal("Get() over an invalid override = nil error, want a parse error")
	}
}

// TestParseRejectsContractBreaks pins the three ways a file can lie about
// itself: a name that disagrees with its path, an undeclared variable, and a
// declared variable the body never uses.
func TestParseRejectsContractBreaks(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"name mismatch": `---
name: other/name
description: "x"
version: 1
variables: []
---
Do the thing.`,
		"undeclared variable": `---
name: test/one
description: "x"
version: 1
variables: []
---
Do the {{thing}}.`,
		"unused variable": `---
name: test/one
description: "x"
version: 1
variables: [thing]
---
Do it.`,
		"no description": `---
name: test/one
version: 1
variables: []
---
Do it.`,
	}
	for name, raw := range cases {
		if _, err := Parse("test/one", []byte(raw)); err == nil {
			t.Errorf("%s: Parse() = nil error, want rejection", name)
		}
	}
}

// TestComposerRecordsEveryPromptItRendered is the instrumentation contract: a
// composed prompt names the files (and versions) that produced it, so an
// evaluation can group results by prompt revision instead of by deployment.
func TestComposerRecordsEveryPromptItRendered(t *testing.T) {
	c := &Composer{}
	c.Text("genui/closing-rule")
	c.MustRender("genui/envelope", map[string]string{"components": "text_note {text}"})

	parts := c.Parts()
	if len(parts) != 2 {
		t.Fatalf("Parts() = %v, want two entries", parts)
	}
	if parts[0].ID != "genui/closing-rule" || parts[1].ID != "genui/envelope" {
		t.Fatalf("Parts() = %v, want ids in composition order", parts)
	}
	for _, p := range parts {
		if p.Version < 1 {
			t.Errorf("%s has version %d, want >= 1", p.ID, p.Version)
		}
	}
	embedded, err := Get("genui/envelope")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if parts[1].Version != embedded.Version {
		t.Errorf("recorded version = %d, want the file's %d", parts[1].Version, embedded.Version)
	}
}

// TestComposerJoinShape pins the text contract two ways: fragments are joined by
// one space, and a blank fragment contributes nothing (a disabled surface must
// not leave a gap).
func TestComposerJoinShape(t *testing.T) {
	c := &Composer{}
	c.Add("one")
	c.Add("")
	c.Add("two")
	c.Append("\nUser request: hi")

	if got, want := c.String(), "one two\nUser request: hi"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

// TestRenderMetaReturnsTheFilesAddress pins the single-file form used by call
// sites that send one prompt (ChatSuggest): the text, the id and the version
// travel together, so a trace cannot name a prompt with the wrong revision.
func TestRenderMetaReturnsTheFilesAddress(t *testing.T) {
	text, meta, err := RenderMeta("chat/suggest-todo", map[string]string{"partial": `"buy"`})
	if err != nil {
		t.Fatalf("RenderMeta: %v", err)
	}
	if text == "" {
		t.Fatal("RenderMeta returned empty text")
	}
	if meta.ID != "chat/suggest-todo" {
		t.Errorf("meta.ID = %q", meta.ID)
	}
	embedded, err := Get("chat/suggest-todo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if meta.Version != embedded.Version {
		t.Errorf("meta.Version = %d, want %d", meta.Version, embedded.Version)
	}
}
