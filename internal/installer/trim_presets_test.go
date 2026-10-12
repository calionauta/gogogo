// SCOPE:plugin — installer engine: trim-preset sync tests
package installer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestTrimPresetsReferenceKnownUnits pins every preset drop id to a real
// manifest unit. Renaming/removing a unit without updating trim_presets.go
// fails here naming the stale id — update the preset lists.
func TestTrimPresetsReferenceKnownUnits(t *testing.T) {
	known := map[string]bool{}
	for _, u := range manifestUnits {
		known[u.id] = true
	}
	for name, p := range trimPresets {
		for _, id := range p.Drop {
			if !known[id] {
				t.Errorf("preset %q drops unknown unit %q: update trim_presets.go (valid: %v)",
					name, id, dropIDs(manifestUnits))
			}
		}
		if p.Idea == "" {
			t.Errorf("preset %q has no Idea line (needed for --help/plan output)", name)
		}
	}
}

// TestTrimPresetsCoverNewUnits is the sync mechanism: the union of all
// preset drops must equal every manifest unit. Adding a unit without
// deciding its preset fate fails here — add it to at least one preset's
// Drop list (or extend the preset set) and the test goes green.
func TestTrimPresetsCoverNewUnits(t *testing.T) {
	covered := map[string]bool{}
	for _, p := range trimPresets {
		for _, id := range p.Drop {
			covered[id] = true
		}
	}
	for _, u := range manifestUnits {
		if !covered[u.id] {
			t.Errorf("unit %q not dropped by any preset: decide its fate in trim_presets.go "+
				"(minimal should drop everything offered; targeted presets drop their own)",
				u.id)
		}
	}
}

// TestTrimPresetPlansMatchLists guards the kind-dimension trap: planTrim
// drops by kind (plugin vs feature questions stay independent), so a unit
// whose kind changed might silently survive its preset. The resolved plan
// must drop exactly the listed ids.
func TestTrimPresetPlansMatchLists(t *testing.T) {
	for name, p := range trimPresets {
		plan, ok := presetPlan(name)
		if !ok {
			t.Fatalf("presetPlan(%q) unknown, want known", name)
		}
		got := dropIDs(plan)
		slices.Sort(got)
		want := slices.Clone(p.Drop)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("preset %q resolves to drops %v, want %v "+
				"(a unit's kind likely changed — check its capability Kind)",
				name, got, want)
		}
	}
	if _, ok := presetPlan("does-not-exist"); ok {
		t.Error("presetPlan(unknown) = ok, want unknown (caller prints preset names as hint)")
	}
}

// TestRemovedPathsListsOwnedPaths pins the removed-paths: companion line:
// exactly the dropped units' owned dirs+files, sorted, so the registry
// conformance tests can skip them without importing the installer.
func TestRemovedPathsListsOwnedPaths(t *testing.T) {
	got := removedPaths([]trimUnit{findUnit(t, "genui")})
	for _, want := range []string{
		"features/chat", "features/genui", "internal/server/genui_stubs_test.go",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("removedPaths(genui) missing %q, got %q", want, got)
		}
	}
	if got := removedPaths(nil); got != "none" {
		t.Errorf("removedPaths(nil) = %q, want none", got)
	}
	dir := t.TempDir()
	if err := writeAgents(dir, "my-app", []trimUnit{findUnit(t, "genui")}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(raw), "removed-paths: ") {
		t.Errorf("AGENTS.md missing the removed-paths: line:\n%s", raw)
	}
	if !strings.Contains(string(raw), "features/chat") {
		t.Errorf("AGENTS.md removed-paths missing genui bundle dirs:\n%s", raw)
	}
}

// TestTrimPresetCompositionsBuild gates what the per-unit gate cannot:
// combined trims. Single units leave other consumers of shared declarations
// (bootCtx, test cfg) behind; a preset dropping them all at once exposed
// exactly that (undeclared-identifier vet failures on the minimal tree).
// no-collab needs no entry: it is one unit, already gated per unit.
func TestTrimPresetCompositionsBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("copies and builds the whole repo per preset")
	}
	for _, name := range []string{"minimal", "no-ai"} {
		t.Run(name, func(t *testing.T) {
			drop, ok := presetPlan(name)
			if !ok {
				t.Fatalf("presetPlan(%q) unknown", name)
			}
			dir := t.TempDir()
			copyRepoForTrim(t, dir)
			rc := &Receipt{}
			if err := applyTrim(dir, drop, rc); err != nil {
				t.Fatalf("trim: %v", err)
			}
			for _, unit := range rc.Units {
				if len(unit.StripsMissed) > 0 {
					t.Errorf("preset %q unit %q missed strips:\n  %s",
						name, unit.ID, strings.Join(unit.StripsMissed, "\n  "))
				}
			}
			ctx := context.Background()
			for _, step := range [][]string{
				{"go", "tool", "templ", "generate"},
				{"gofmt", "-w", "."},
			} {
				//nolint:gosec // step is a literal from this file, not caller input.
				cmd := exec.CommandContext(ctx, step[0], step[1:]...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s failed in %s tree:\n%s", strings.Join(step, " "), name, out)
				}
			}
			cmd := exec.CommandContext(ctx, "go", "vet", "./...")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s tree does not vet:\n%s", name, out)
			}
			cmd = exec.CommandContext(ctx,
				"go", "test", "./internal/capabilities/",
				"-run", "TestTemplAPIURLsHaveRoutes", "-count=1")
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s tree lost a route:\n%s", name, out)
			}
		})
	}
}

// TestAddManualWiringPinsReversibility pins what `add` can and cannot
// replay: skins-extra is fully mechanical (anchored spans, imports),
// everything with bare chi.go constructions, test-body edits, one-way
// replaces or unanchored strips refuses with the exact lines. Add a unit
// whose wiring is span-anchored and it joins the reversible set here;
// extend a unit with a bare drop and it must appear in the refuse list.
func TestAddManualWiringPinsReversibility(t *testing.T) {
	if got := addManualWiring(findUnit(t, unitSkinsExtra)); len(got) != 0 {
		t.Errorf("skins-extra manual = %v, want fully reversible", got)
	}
	for _, id := range []string{
		unitLanding, unitConfigView, unitWhiteboard, unitGoAkt,
		unitCredits, unitGenUI, unitDagnats, unitSounds,
	} {
		got := addManualWiring(findUnit(t, id))
		if len(got) == 0 {
			t.Errorf("unit %q reports fully reversible — add would write code the proof rejects", id)
		}
	}
	// The landing recipe names the construction the mux anchor cannot
	// place (the proof failure that motivated the gate).
	found := false
	for _, line := range addManualWiring(findUnit(t, unitLanding)) {
		if strings.Contains(line, "landingchi.NewHandler(cfg, a),") {
			found = true
		}
	}
	if !found {
		t.Errorf("landing manual lines miss the construction: %v", addManualWiring(findUnit(t, unitLanding)))
	}
}

// TestPresetKeepListsPinsResolution pins preset → keep-list resolution:
// minimal empties both dimensions (keepNone, not "" — empty means
// keep-all), targeted presets keep everything they do not drop, and
// unknown names fail listing the valid presets.
func TestPresetKeepListsPinsResolution(t *testing.T) {
	plugins, features, err := presetKeepLists(presetMinimal)
	if err != nil {
		t.Fatalf("presetKeepLists(minimal) = %v", err)
	}
	if plugins != keepNone || features != keepNone {
		t.Errorf("minimal keep = (%q, %q), want (none, none)", plugins, features)
	}
	plugins, features, err = presetKeepLists(presetNoAI)
	if err != nil {
		t.Fatalf("presetKeepLists(no-ai) = %v", err)
	}
	drop := dropIDs(planTrim(parseKeep(plugins, features)))
	slices.Sort(drop)
	if !slices.Equal(drop, []string{"credits", "genui"}) {
		t.Errorf("no-ai drops = %v, want [credits genui]", drop)
	}
	if _, _, err := presetKeepLists("does-not-exist"); err == nil {
		t.Error("presetKeepLists(unknown) = nil, want error listing presets")
	} else if !strings.Contains(err.Error(), "minimal") {
		t.Errorf("unknown-preset error must list valid presets: %v", err)
	}
}

// TestTrimMinimalKeepsTodoAndAuth documents the minimal contract: after
// the minimal preset, the todo reference and the auth middleware survive
// (they are feature/core respectively, outside every offered unit).
func TestTrimMinimalKeepsTodoAndAuth(t *testing.T) {
	plan, ok := presetPlan(presetMinimal)
	if !ok {
		t.Fatal("presetPlan(minimal) unknown")
	}
	for _, u := range plan {
		for _, dir := range u.meta().dirs {
			if dir == "features/todo" || dir == "internal/auth" || dir == "internal/db" {
				t.Errorf("minimal preset drops %s via unit %q: todo/auth/db must survive",
					dir, u.id)
			}
		}
	}
}
