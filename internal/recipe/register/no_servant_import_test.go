// The seam must not name a servant.
package register

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// TestSeamImportsNoServant pins the rule that made a scaffold unbuildable: this
// package is the ONE seam core (the recipe engine + composition root) shares with
// recipe code. While it imported internal/collab — a SERVANT — a product that
// dropped every collaborative feature failed `go mod tidy` with "finding module
// for package .../internal/collab", and loro-go (a CGO binding) stayed pinned,
// demanding a C toolchain on every target that asked for none of it.
//
// The transport now travels as register.Deps.Collab (an opaque handle) and a
// recipe asserts it back to collab.Handoff. A future field that names a servant
// type directly would silently restore the defect, so it is rejected here rather
// than discovered by a user's first `go mod tidy`.
func TestSeamImportsNoServant(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed: cannot locate the package to scan")
	}
	dir := filepath.Dir(thisFile)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	// A servant is safe for this seam to name ONLY when the registry gives it a
	// RuntimeOff: those are switched off by env var and never removed, so the
	// import can never be unsatisfiable (internal/llm is the case — the suggest
	// button hides without GOAI_API_KEY). A servant WITHOUT RuntimeOff is a
	// removal target, and naming it here is what makes a dropped product
	// untidyable. The list is derived from the registry rather than restated, so
	// a servant that gains or loses RuntimeOff moves in and out of this gate by
	// itself.
	// Scoped to the transport, the one servant this seam actually carried. The
	// registry has other removable servants the seam still names (internal/events
	// is the live example: Deps.Events is *events.Bus, read through a nil-safe
	// Emit). Inverting those is the same work and deserves its own change; a gate
	// that failed on all of them today would be red on main and get deleted, which
	// protects nothing. Anything added here should move that package to an opaque
	// handle first.
	forbidden := removableServants(t, "internal/collab")
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range f.Imports {
			imported, uerr := strconv.Unquote(spec.Path.Value)
			if uerr != nil {
				continue
			}
			for _, bad := range forbidden {
				if strings.HasSuffix(imported, bad) {
					t.Errorf("%s imports %s: the seam is CORE and must not name a servant — "+
						"pass it as an opaque handle on Deps instead", name, imported)
				}
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned no files: this test would pass vacuously")
	}
}

// removableServants returns the import path suffixes of every servant the
// registry expects to be REMOVED (no RuntimeOff), which this seam may not name.
// Only the named packages are gated; each must be a servant the registry expects
// to REMOVE (no RuntimeOff), so the gate keeps following the registry rather than
// a list that goes stale.
func removableServants(t *testing.T, dirs ...string) []string {
	t.Helper()
	byDir := map[string]capabilities.Capability{}
	for _, c := range capabilities.All {
		for _, d := range c.Dirs {
			byDir[d] = c
		}
	}
	for _, d := range dirs {
		c, ok := byDir[d]
		if !ok {
			t.Fatalf("gated package %s is not a registered capability: the gate is stale", d)
		}
		if c.Kind == capabilities.KindServant && c.RuntimeOff != "" {
			t.Fatalf("gated package %s has RuntimeOff (%q): it is switched off, never "+
				"removed, so naming it in this seam is safe and the gate is wrong", d, c.RuntimeOff)
		}
	}
	return dirs
}
