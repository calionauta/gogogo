// SCOPE:plugin — installer chi.go template sync guard.
//
// The trim/add engine strips and restores chi.go constructions by exact
// substring (extraDrops substrs / routerCallLines). When chi.go's wiring
// changes (e.g. the single-hub fix: `chiQ, hub` instead of
// `queue.NewSSEHub()`), a stale template silently reverts the fix on the
// next trim+add cycle — and no suite failed, because nothing compared the
// templates against the live file. This test pins that every chi.go
// substring every unit owns actually occurs in the live chi.go.
package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTrimSubstrsMatchLiveChiGo pins template/source sync: each unit's
// chi.go drop substring must occur verbatim in the live chi.go, or trim
// strips nothing and add restores a line the tree no longer has.
func TestTrimSubstrsMatchLiveChiGo(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "server", "chi.go"))
	if err != nil {
		t.Fatalf("read live chi.go: %v", err)
	}
	src := string(raw)
	for _, u := range manifestUnits {
		for _, ed := range u.extraDrops {
			if ed.path != chiGoFile {
				continue
			}
			for _, sub := range ed.substrs {
				if strings.HasSuffix(sub, `"`) {
					continue // import-lookalike, restored by addImports
				}
				if !strings.Contains(src, sub) {
					t.Errorf("unit %q: chi.go drop %q not found in live %s (template drift)",
						u.id, sub, chiGoFile)
				}
			}
		}
	}
}
