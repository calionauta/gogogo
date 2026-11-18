package main

import (
	"context"
	"strings"
	"testing"
)

// TestReadonlyRailReachesEveryMutatingTool pins the property docs/mcp.md
// promises: GOGOGO_READONLY=1 makes an agent an advisor, not a builder.
//
// The rail lives in the installer (guardMutable), not in the handlers, so it
// holds by construction only as long as every write path calls it. It did not:
// `recipe install` had no guard at all and walked straight past
// GOGOGO_READONLY=1, which is the documented way to hand an agent write-free
// access to a checkout. That is the class of hole this test exists for — a
// handler that bypasses installer.Run would pass every other test here.

// The arguments are deliberately shaped so a MISSING rail produces a different
// error, not a passing no-op: each call names a real mode of writing.
func TestReadonlyRailReachesEveryMutatingTool(t *testing.T) {
	t.Setenv("GOGOGO_READONLY", "1")
	t.Setenv("GOGOGO_ALLOWED_ROOTS", "")

	dir := t.TempDir()
	// Arguments sufficient to reach the write for each mutating tool. The
	// point is not the specific failure message, only that it refuses.
	cases := []struct {
		tool string
		call func() error
	}{
		{"scaffold_apply", func() error {
			_, _, err := handleScaffoldApply(context.Background(), nil, scaffoldArgs{
				Name: "readonly-probe", Dir: dir, Confirm: true,
			})
			return err
		}},
		{"new_feature", func() error {
			_, _, err := handleNewFeature(context.Background(), nil, newFeatureArgs{
				Name: "probe", Dir: dir, Confirm: true,
			})
			return err
		}},
		{"recipe_install", func() error {
			_, _, err := handleRecipeInstall(context.Background(), nil, recipeArgs{
				Src: "todo", Dir: dir, Confirm: true,
			})
			return err
		}},
		{"recipe_uninstall", func() error {
			_, _, err := handleRecipeUninstall(context.Background(), nil, recipeArgs{
				ID: "todo", Dir: dir, Confirm: true,
			})
			return err
		}},
	}

	for _, tc := range cases {
		err := tc.call()
		if err == nil {
			t.Errorf("%s: succeeded under GOGOGO_READONLY=1 — the rail never reached it", tc.tool)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), "read-only") {
			t.Errorf("%s: refused with %q, which does not name the read-only rail; "+
				"an operator cannot tell a guard from a bug", tc.tool, err)
		}
	}
}

// TestReadonlyRailLeavesReadToolsWorking is the other half: a rail that breaks
// advise_stack is not a rail, it is an outage. The advisor is the whole reason
// an operator sets READONLY in the first place.
func TestReadonlyRailLeavesReadToolsWorking(t *testing.T) {
	t.Setenv("GOGOGO_READONLY", "1")

	if _, _, err := handleAdvise(context.Background(), nil, adviseArgs{
		Need: "an internal tool with logins", Format: "json",
	}); err != nil {
		t.Errorf("advise_stack failed under read-only: %v — the advisor is the point of the rail", err)
	}
	res, _, err := handleCapabilitiesList(context.Background(), nil, capabilitiesArgs{})
	if err != nil {
		t.Errorf("capabilities_list failed under read-only: %v", err)
	} else if res == nil || len(res.Content) == 0 {
		t.Error("capabilities_list returned no content")
	}
}
