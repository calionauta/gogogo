// SCOPE:plugin — the panel registry's extensibility contract: a NEW panel is a
// provider and nothing else.
//
// This is the test that keeps the seam honest. The old design made
// features/genui/components the hub every feature had to edit (a shared
// `panels` map plus the boot slice), which is the coupling a feature slice
// exists to avoid. The claim now is: implementing PanelProvider is sufficient —
// no map in this package, no switch in the renderer, no edit anywhere else.
//
// A test can only prove that by BOOTING with a second provider and watching the
// prompt, the router's candidate set, parsing, hydration and rendering all pick
// it up. That is what secondStubPanel below does — and because the registry
// freezes the vocabulary at boot on purpose (a later change is refused), the
// proof runs in a subprocess booted with GOGOGO_TEST_TWO_PANELS=1 rather than by
// relaxing the guard for the test.
package components

import (
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// secondStubPanel is a panel that did not exist before this file: a different
// name, a different writable prop, a different render. It exists only to prove
// the registry needs no edit to admit it.
type secondStubPanel struct{}

func (secondStubPanel) Component() string { return "notes_list" }

// secondStubPanel declares its OWN aliases, and that is the generic-seam proof:
// the shared catalog knows nothing about notes, so this panel's synonyms resolve
// only because its provider announced them. Before the alias table moved behind
// PanelAliaiser, this line could not exist — every alias in the system was
// hardcoded to todo_list.
func (secondStubPanel) Aliases() []string { return []string{"notes", "note"} }

func (secondStubPanel) Prompt() string     { return "notes_list {limit} — the user's own recent notes" }
func (secondStubPanel) Writable() []string { return []string{"limit"} }
func (secondStubPanel) Fill(_ context.Context, _ string, props map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(props)+1)
	maps.Copy(out, props)
	out["titles"] = []string{"a note from the app's store"}
	return out, nil
}

func (secondStubPanel) Render(props map[string]any) (templ.Component, error) {
	limit, _ := props["limit"].(string)
	titles, _ := props["titles"].([]string)
	var b strings.Builder
	fmt.Fprintf(&b, `<div id="notes-panel" data-limit="%s">`, limit)
	for _, title := range titles {
		b.WriteString("<span>" + title + "</span>")
	}
	b.WriteString("</div>")
	return templ.Raw(b.String()), nil
}

// TestAddingAPanelNeedsOnlyAProvider is the extensibility claim, end to end.
//
// It runs in the SUBPROCESS booted with GOGOGO_TEST_TWO_PANELS=1 (see TestMain
// and the guard below), because the registry freezes the vocabulary at boot and
// refuses a later change — so the only faithful way to show that a second panel
// needs nothing but a provider is to boot with one. Everything after the guard
// then asserts the new panel is live: prompt, router candidates, parse (its own
// writable prop, and rejection of its own server-owned field), hydrate, render.
func TestAddingAPanelNeedsOnlyAProvider(t *testing.T) {
	if os.Getenv("GOGOGO_TEST_TWO_PANELS") != "1" {
		t.Skip("runs in the two-panel subprocess (see TestMain); the sibling test that " +
			"spawns it is TestSecondPanelBecomesLiveInAFreshBoot")
	}
	if !IsPanel("notes_list") {
		t.Fatal("the extra provider was not installed — the boot contract is broken")
	}
	// Its aliases came from the provider, not from a table in this package: a
	// feature the catalog has never heard of is reachable by the words a model
	// would actually write.
	for _, alias := range []string{"notes", "note"} {
		if !IsPanel(alias) {
			t.Errorf("alias %q does not resolve — aliases are not provided by the panel", alias)
		}
	}

	// 1. The prompt advertises it (name and its own guidance line).
	chat := PromptForChat()
	if !strings.Contains(chat, "notes_list") {
		t.Error("chat prompt does not advertise the new panel")
	}
	if !strings.Contains(chat, "the user's own recent notes") {
		t.Error("the new panel's own prompt line did not reach the prompt")
	}

	// 2. The router can offer it, on both legs.
	for _, set := range [][]Surface{NormalizeSurfaces(DefaultSurfacesAsk), NormalizeSurfaces(DefaultSurfacesChat)} {
		if !slices.Contains(CatalogComponentsFor(set), "notes_list") {
			t.Errorf("router candidates for %v lack the new panel", Names(set))
		}
	}

	// 3. It parses with its OWN writable prop...
	dirs, err := ParseDirectives(`{"components":[{"type":"notes_list","props":{"limit":"5"}}]}`)
	if err != nil {
		t.Fatalf("the new panel's own prop was rejected: %v", err)
	}
	// ...and rejects a server-owned field, from its own declaration alone.
	if _, err := ParseDirectives(`{"components":[{"type":"notes_list","props":{"titles":["invented"]}}]}`); err == nil {
		t.Error("the new panel accepted a server-owned prop — the writable list is not per-panel")
	}

	// 4. Hydration fills it (no provider list passed: the registry is the source).
	hydrated, herr := Hydrate(context.Background(), dirs, "owner")
	if herr != nil {
		t.Fatalf("hydrate: %v", herr)
	}
	if len(hydrated) != 1 {
		t.Fatalf("hydrated to %d directives, want 1", len(hydrated))
	}

	// 5. It renders through its own provider, with no renderer-table entry.
	comps, rerr := RenderAll(hydrated)
	if rerr != nil {
		t.Fatalf("render: %v", rerr)
	}
	if len(comps) != 1 {
		t.Fatalf("rendered %d components, want 1", len(comps))
	}
	var buf strings.Builder
	if err := comps[0].Render(context.Background(), &buf); err != nil {
		t.Fatalf("render component: %v", err)
	}
	if !strings.Contains(buf.String(), "a note from the app's store") {
		t.Errorf("the new panel's renderer did not run: %s", buf.String())
	}
}

// TestSecondPanelBecomesLiveInAFreshBoot boots this package's own test binary a
// second time with the extra provider, and requires the extensibility assertions
// to pass THERE. It is the parent half of the pair: without it the subprocess
// test would be skipped forever and the claim would be unproven.
func TestSecondPanelBecomesLiveInAFreshBoot(t *testing.T) {
	if os.Getenv("GOGOGO_TEST_TWO_PANELS") == "1" {
		t.Skip("this is the parent side; the child runs TestAddingAPanelNeedsOnlyAProvider")
	}
	//nolint:gosec // os.Args[0] is the test binary's own path, not user input:
	// re-execing the test binary is the point of the fresh-boot proof.
	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=TestAddingAPanelNeedsOnlyAProvider", "-test.v")
	cmd.Env = append(os.Environ(), "GOGOGO_TEST_TWO_PANELS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("a boot with a second panel provider failed:\n%s", out)
	}
	if !strings.Contains(string(out), "--- PASS") {
		t.Fatalf("the child did not report the extensibility assertions as passing:\n%s", out)
	}
}

// TestInstallRegistryRejectsHalfPanels pins the three boot-time invariants, each
// of which would otherwise become a runtime surprise: an empty name, two
// providers claiming one name, and a panel with no writable prop (which would
// make every model answer a shape error).
//
// It calls validatePanelProviders (the pure half of the install) rather than
// InstallPanelRegistry, so the assertion is local and the test mutates NOTHING.
// The earlier version relied on validation happening before the registry was
// touched, which was true but made this test's correctness depend on statement
// order inside another function.
func TestInstallRegistryRejectsHalfPanels(t *testing.T) {
	t.Parallel()
	for name, providers := range map[string][]PanelProvider{
		"empty name":      {secondStubPanelNamed("")},
		"no writable":     {secondStubPanelNamed("half_panel")},
		"duplicate names": {stubTestPanel{}, secondStubPanelNamed("todo_list")},
	} {
		if _, _, err := validatePanelProviders(providers); err == nil {
			t.Errorf("%s: accepted, want a boot error", name)
		}
	}
	// The valid set must pass, so the guard is not rejecting everything.
	if _, names, err := validatePanelProviders([]PanelProvider{stubTestPanel{}, secondStubPanel{}}); err != nil {
		t.Errorf("a valid set was rejected: %v", err)
	} else if len(names) != 2 || !sort.StringsAreSorted(names) {
		t.Errorf("names = %v, want two sorted", names)
	}
}

// TestInstallRegistryRefusesAVocabularyChange pins the guard that matters: a
// second install with a DIFFERENT panel set must fail, because it would change
// what the model may emit while answers are in flight. A second install with the
// SAME set is fine — that is what lets BootChi run more than once in a test
// process.
//
// It re-installs the SET TestMain installed (same names, so a re-bind) rather
// than a set of its own: the guard is about the vocabulary CHANGING, and any
// other set would be a change by definition. Not parallel, since its subject is
// the mutation path itself.
func TestInstallRegistryRefusesAVocabularyChange(t *testing.T) {
	// Capacity 4: the set plus the one panel the change below tries to add.
	packageSet := make([]PanelProvider, 0, 4)
	packageSet = append(packageSet, stubTestPanel{}, failingStubPanel{}, digestStubPanel{})
	// Same set: allowed (idempotent).
	if err := InstallPanelRegistry(packageSet); err != nil {
		t.Fatalf("re-installing the same vocabulary failed: %v", err)
	}
	// Different set: refused, and it must leave the vocabulary alone.
	if err := InstallPanelRegistry(append(packageSet, secondStubPanel{})); err == nil {
		t.Error("a vocabulary change was accepted, want a refusal")
	}
	if !IsPanel(panelNameInTest) || !IsPanel(failingPanelName) {
		t.Error("the refused install changed the vocabulary anyway")
	}
	if IsPanel(secondStubPanel{}.Component()) {
		t.Error("the refused install registered the new panel anyway")
	}
}

// secondStubPanelNamed builds a variant with a chosen name, for the negative
// cases. An empty name and an empty writable list are the two malformed shapes.
func secondStubPanelNamed(name string) PanelProvider {
	if name == "" {
		return emptyNamePanel{}
	}
	if name == "half_panel" {
		return noWritablePanel{}
	}
	return namedPanel{name: name}
}

type emptyNamePanel struct{}

func (emptyNamePanel) Component() string { return "" }
func (emptyNamePanel) Prompt() string    { return "x" }
func (emptyNamePanel) Writable() []string {
	return []string{"x"}
}

func (emptyNamePanel) Fill(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (emptyNamePanel) Render(map[string]any) (templ.Component, error) {
	return templ.Raw("<div></div>"), nil
}

type noWritablePanel struct{}

func (noWritablePanel) Component() string { return "half_panel" }
func (noWritablePanel) Prompt() string    { return "x" }
func (noWritablePanel) Writable() []string {
	return nil
}

func (noWritablePanel) Fill(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (noWritablePanel) Render(map[string]any) (templ.Component, error) {
	return templ.Raw("<div></div>"), nil
}

type namedPanel struct{ name string }

func (n namedPanel) Component() string { return n.name }
func (namedPanel) Prompt() string      { return "x" }
func (namedPanel) Writable() []string {
	return []string{"x"}
}

func (namedPanel) Fill(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func (namedPanel) Render(map[string]any) (templ.Component, error) {
	return templ.Raw("<div></div>"), nil
}
