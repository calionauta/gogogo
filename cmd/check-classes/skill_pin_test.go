package main

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/calionauta/daisyui-lint/daisyui"
)

// The vendored skill and the pinned linter are two copies of one upstream
// artifact: the skill is what agents read and what the gate lints against, and
// the linter carries a snapshot generated from the same upstream text. The only
// difference that needs action is the LINTER BEING AHEAD — it would then enforce
// a vocabulary the documentation agents read does not contain, which is a false
// positive waiting to happen. The reverse (the skill ahead of the snapshot) is
// normal: the skill is refreshed weekly, the module on demand.
func TestPinnedLinterKnowsNoMoreThanTheVendoredSkill(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")

	pinned, linterVersion, err := daisyui.LoadBundled()
	if err != nil {
		t.Fatalf("load the pinned linter's bundled vocabulary: %v", err)
	}
	skillPath, err := daisyui.FindSkill(root)
	if err != nil {
		t.Skipf("no vendored skill in this tree: %v", err)
	}
	repo, err := daisyui.LoadSkillFile(skillPath)
	if err != nil {
		t.Fatalf("load %s: %v", skillPath, err)
	}

	var missing []string
	for base := range pinned {
		if repo[base] == nil {
			missing = append(missing, base)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("the pinned daisyui-lint (%s) declares %d component(s) the vendored skill (%s) does not: %s\n"+
			"run scripts/refresh-daisyui.sh and pin a linter built from the same upstream text",
			linterVersion, len(missing), daisyui.SkillVersion(skillPath), strings.Join(missing, ", "))
	}
}
