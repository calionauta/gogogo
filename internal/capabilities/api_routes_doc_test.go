// SCOPE:core — generated route inventory (drift test).
//
// docs/api-routes.md is generated from source, not written by hand: the
// registered routes and the template/script refs the api-routes contract
// test already scans. A hand-kept inventory rots (the dagnats trim proved
// it: sources moved, prose didn't); a generated one cannot.
//
// Regenerate with:
//
//	go test ./internal/capabilities/ -run TestApiRoutesDocFresh -update
package capabilities

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var apiRoutesDocUpdate = flag.Bool("update", false, "rewrite docs/api-routes.md from source")

// TestApiRoutesDocFresh pins the generated inventory: what is on disk must
// equal what the scanners see. On mismatch it fails naming the exact
// command that fixes it — never "edit the doc", which would reintroduce
// the drift this file exists to prevent.
func TestApiRoutesDocFresh(t *testing.T) {
	root := repoRoot(t)
	routes := registeredAPIRoutes(t, root)
	refs := templAPIRefs(t, root)
	want := renderAPIRoutesDoc(routes, refs)
	path := filepath.Join(root, "docs", "api-routes.md")
	if *apiRoutesDocUpdate {
		if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with -update)", path, err)
	}
	if string(raw) != want {
		t.Fatalf("docs/api-routes.md is stale (%d routes, %d refs on disk):\n"+
			"regenerate with go test ./internal/capabilities/ -run TestApiRoutesDocFresh -update",
			len(routes), len(refs))
	}
}

// renderAPIRoutesDoc renders the inventory. Deterministic by construction:
// both scanners return sorted lists, and there is no timestamp (a timestamp
// would dirty the file on every run and teach everyone to ignore the diff).
func renderAPIRoutesDoc(routes, refs []string) string {
	var b strings.Builder
	b.WriteString("# API routes\n\n")
	b.WriteString("> Generated from source — do not edit by hand.\n")
	b.WriteString("> Regenerate: `go test ./internal/capabilities/ -run TestApiRoutesDocFresh -update`\n")
	b.WriteString("> Coverage is enforced by TestTemplAPIURLsHaveRoutes (every ref below\n")
	b.WriteString("> must match a registered route); this file is the inventory it proves.\n\n")
	fmt.Fprintf(&b, "## Registered routes (%d)\n\n", len(routes))
	b.WriteString("Every `/api/*` literal in non-test, non-generated Go sources.\n\n")
	for _, r := range routes {
		b.WriteString("- `" + r + "`\n")
	}
	fmt.Fprintf(&b, "\n## Template and script refs (%d)\n\n", len(refs))
	b.WriteString("Every `/api/*` URL fired from `.templ` and `.js` sources.\n\n")
	for _, r := range refs {
		b.WriteString("- `" + r + "`\n")
	}
	return b.String()
}
