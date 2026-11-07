package installer

// Manifest tests: name validation, the export unit index, and the plan
// shape. The old keep/drop tests died with the trim operation they pinned.

import (
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"my-app", "app2", "a.b_c-d"} {
		if err := validateName(ok); err != nil {
			t.Errorf("validateName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "-app", "my app", "gogogo"} {
		if err := validateName(bad); err == nil {
			t.Errorf("validateName(%q) = nil, want error", bad)
		}
	}
}

// TestManifestUnitsCoverTheRegistry pins the export index: every manifest
// unit maps to real registry capabilities, and the scaffold plan lists
// everything (there is no keep/drop split any more).
func TestManifestUnitsCoverTheRegistry(t *testing.T) {
	byID := capabilities.ByID()
	seen := map[string]bool{}
	for _, u := range builtinUnits {
		if seen[u.id] {
			t.Errorf("duplicate unit %q", u.id)
		}
		seen[u.id] = true
		for _, capID := range u.caps() {
			if _, ok := byID[capID]; !ok {
				t.Errorf("unit %q references unknown capability %q", u.id, capID)
			}
		}
	}
	p := buildPlan("x", "y", ".", true)
	if len(p.Drop) != 0 {
		t.Errorf("plan drops %v, want nothing", p.Drop)
	}
	var b strings.Builder
	printPlanText(&b, p)
	if !strings.Contains(b.String(), "removal:        none (full template)") {
		t.Errorf("plan text must say the trim is gone:\n%s", b.String())
	}
}
