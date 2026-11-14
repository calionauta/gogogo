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
	p := buildPlan("x", "y", ".", "", true)
	if len(p.Keep) != len(capabilities.All) {
		t.Errorf("plan keeps %d units, want all %d", len(p.Keep), len(capabilities.All))
	}
	var b strings.Builder
	printPlanText(&b, p)
	if !strings.Contains(b.String(), "removal:        none (full template") {
		t.Errorf("plan text must say nothing is removed:\n%s", b.String())
	}
	// The text groups by the same three kind words the JSON carries, so a
	// reader cannot see one vocabulary in the plan and another in the registry.
	for _, kind := range []capabilities.Kind{capabilities.KindCore, capabilities.KindServant, capabilities.KindFeature} {
		if !strings.Contains(b.String(), string(kind)+": ") {
			t.Errorf("plan text is missing the %q group:\n%s", kind, b.String())
		}
	}
}
