package installer

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// TestExtensionDescriptorsCarryDependentsAndState is the inverse-property gate:
// every DependsOn edge must appear flipped in someone's Dependents, and every
// descriptor must carry a non-empty State. A descriptor that omits either is a
// discovery surface an agent cannot plan from.
func TestExtensionDescriptorsCarryDependentsAndState(t *testing.T) {
	exts, err := extensionDescriptors(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("descriptors: %v", err)
	}
	if len(exts) == 0 {
		t.Fatal("no descriptors")
	}
	byID := map[string]extensionDescriptor{}
	for _, e := range exts {
		byID[e.ID] = e
		if e.State == "" {
			t.Errorf("descriptor %q has no state", e.ID)
		}
	}
	for _, c := range capabilities.All {
		e, ok := byID[c.ID]
		if !ok {
			t.Fatalf("capability %q missing from descriptors", c.ID)
		}
		if !slices.Equal(slices.Sorted(slices.Values(e.DependsOn)), slices.Sorted(slices.Values(c.DependsOn))) {
			t.Errorf("%q dependsOn drifted: %v vs %v", c.ID, e.DependsOn, c.DependsOn)
		}
		for _, dep := range c.DependsOn {
			if !slices.Contains(byID[dep].Dependents, c.ID) {
				t.Errorf("%q must list %q in dependents (inverse of dependsOn)", dep, c.ID)
			}
		}
	}
}

// TestExtensionDescriptorsAdvertiseBuiltinVerbs proves the built-in half
// of discovery: the registry mirror (capability Actions) reaches the
// descriptor even without a receipt, so an agent needs no source read to
// learn that todo can be invoked headless.
func TestExtensionDescriptorsAdvertiseBuiltinVerbs(t *testing.T) {
	exts, err := extensionDescriptors(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("descriptors: %v", err)
	}
	for _, e := range exts {
		if e.ID != "todo" {
			continue
		}
		if !slices.Contains(e.Invoke, "create") {
			t.Fatalf("todo invoke verbs missing: %v", e.Invoke)
		}
		if !slices.Contains(e.InvokeParams["create"], "title") {
			t.Fatalf("todo create params missing: %v", e.InvokeParams)
		}
		return
	}
	t.Fatal("todo descriptor not found")
}

// TestExtensionDescriptorsExposeReceiptSurface proves the receipt half:
// a declaration stored in a receipt must reach the descriptor (routes,
// tables, jobs, slots), or discovery lies about what the recipe claims.
func TestExtensionDescriptorsExposeReceiptSurface(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	yml := `id: probe
upstream: {repo: "github.com/x/y", ref: "v1"}
files: {}
declaration:
  tables: ["ext_probe_cards"]
  routes: [{method: GET, pattern: /api/probe}, {method: POST, pattern: /api/probe}]
  jobs: ["ext:probe:run"]
  slots: []
  actions: [{verb: create, route: {method: POST, pattern: /api/probe}, params: [title]}]
`
	if err := os.WriteFile(filepath.Join(dir, ".recipes", "probe.yml"), []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	exts, err := extensionDescriptors(dir)
	if err != nil {
		t.Fatalf("descriptors: %v", err)
	}
	for _, e := range exts {
		if e.ID != "probe" {
			continue
		}
		if !slices.Contains(e.Tables, "ext_probe_cards") {
			t.Errorf("probe tables missing: %v", e.Tables)
		}
		if len(e.Routes) != 2 || e.Routes[0] != "GET /api/probe" {
			t.Errorf("probe routes missing: %v", e.Routes)
		}
		if !slices.Contains(e.Jobs, "ext:probe:run") {
			t.Errorf("probe jobs missing: %v", e.Jobs)
		}
		if !slices.Contains(e.Invoke, "create") {
			t.Errorf("probe invoke verbs missing: %v", e.Invoke)
		}
		if !slices.Contains(e.InvokeParams["create"], "title") {
			t.Errorf("probe create params missing: %v", e.InvokeParams)
		}
		return
	}
	t.Fatal("probe receipt not found in descriptors")
}
