package installer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
)

// TestRecipeDescriptorsCarryDependentsAndState is the inverse-property gate:
// every DependsOn edge must appear flipped in someone's Dependents, and every
// descriptor must carry a non-empty State. A descriptor that omits either is a
// discovery surface an agent cannot plan from.
func TestRecipeDescriptorsCarryDependentsAndState(t *testing.T) {
	exts, err := recipeDescriptors(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("descriptors: %v", err)
	}
	if len(exts) == 0 {
		t.Fatal("no descriptors")
	}
	byID := map[string]recipeDescriptor{}
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

// TestRecipeDescriptorsAdvertiseBuiltinVerbs proves the built-in half
// of discovery: the registry mirror (capability Actions) reaches the
// descriptor even without a receipt, so an agent needs no source read to
// learn that todo can be invoked headless.
func TestRecipeDescriptorsAdvertiseBuiltinVerbs(t *testing.T) {
	exts, err := recipeDescriptors(filepath.Join("..", ".."))
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
		if e.InvokeDescriptions["create"] == "" {
			t.Fatalf("todo create description missing: %v", e.InvokeDescriptions)
		}
		if e.InvokeParamHelp["create"]["title"] == "" {
			t.Fatalf("todo create param help missing: %v", e.InvokeParamHelp)
		}
		return
	}
	t.Fatal("todo descriptor not found")
}

// TestRecipeDescriptorsExposePathParamsAndOmissions proves the two discovery
// halves an agent needs to call a CRUD recipe: the `{id}` it must supply and
// the mutating routes the author deliberately kept out.
func TestRecipeDescriptorsExposePathParamsAndOmissions(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".recipes"), 0o755); err != nil {
		t.Fatal(err)
	}
	yml := `id: probe
upstream: {repo: "github.com/x/y", ref: "v1"}
files: {}
declaration:
  routes:
  - {method: POST, pattern: "/api/probe/{id}/toggle"}
  - {method: POST, pattern: /api/probe/suggest}
  actions:
  - verb: toggle
    description: Toggle one probe row.
    route: {method: POST, pattern: "/api/probe/{id}/toggle"}
    pathParams: [id]
    paramHelp: {id: The probe UUID.}
  omitted:
  - route: {method: POST, pattern: /api/probe/suggest}
    reason: asynchronous job; the result arrives through the stream
`
	if err := os.WriteFile(filepath.Join(dir, ".recipes", "probe.yml"), []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	exts, err := recipeDescriptors(dir)
	if err != nil {
		t.Fatalf("descriptors: %v", err)
	}
	for _, e := range exts {
		if e.ID != "probe" {
			continue
		}
		if !slices.Contains(e.InvokePathParams["toggle"], "id") {
			t.Fatalf("probe toggle path param missing: %v", e.InvokePathParams)
		}
		if len(e.Omitted) != 1 || !strings.Contains(e.Omitted[0], "/api/probe/suggest") {
			t.Fatalf("probe omission missing: %v", e.Omitted)
		}
		return
	}
	t.Fatal("probe receipt not found in descriptors")
}

// TestRecipeDescriptorsExposeReceiptSurface proves the receipt half:
// a declaration stored in a receipt must reach the descriptor (routes,
// tables, jobs, slots), or discovery lies about what the recipe claims.
func TestRecipeDescriptorsExposeReceiptSurface(t *testing.T) {
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
  metrics:
  - label: Probe rows
    query: "SELECT COUNT(*) FROM ext_probe_cards WHERE owner = ?"
  - kind: outcome
    id: probe_without_repair
    job: Read one probe row without fixing a misread field
    beneficiary: user
    success:
      label: Rows without repair
      query: "SELECT COUNT(*) FROM ext_probe_cards WHERE owner = ? AND corrected = 0"
    total:
      label: Probe rows
      query: "SELECT COUNT(*) FROM ext_probe_cards WHERE owner = ?"
    counterbalance:
    - label: Corrected rows
      query: "SELECT COUNT(*) FROM ext_probe_cards WHERE owner = ? AND corrected = 1"
    window: session
  actions:
  - verb: create
    description: Create one probe row.
    route: {method: POST, pattern: /api/probe}
    params: [title]
    paramHelp: {title: The row's title.}
`
	if err := os.WriteFile(filepath.Join(dir, ".recipes", "probe.yml"), []byte(yml), 0o600); err != nil {
		t.Fatal(err)
	}
	exts, err := recipeDescriptors(dir)
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
		if len(e.Metrics) != 1 || e.Metrics[0].Label != "Probe rows" {
			t.Errorf("probe metrics missing: %v", e.Metrics)
		}
		if len(e.Outcomes) != 1 {
			t.Fatalf("probe outcomes missing: %v", e.Outcomes)
		}
		if o := e.Outcomes[0]; o.ID != "probe_without_repair" || o.TotalLabel != "Probe rows" ||
			o.SuccessLabel != "Rows without repair" || !slices.Contains(o.Counterbalance, "Corrected rows") {
			t.Errorf("probe outcome drifted: %+v", o)
		}
		if e.InvokeDescriptions["create"] != "Create one probe row." {
			t.Errorf("probe create description missing: %v", e.InvokeDescriptions)
		}
		if e.InvokeParamHelp["create"]["title"] != "The row's title." {
			t.Errorf("probe create param help missing: %v", e.InvokeParamHelp)
		}
		return
	}
	t.Fatal("probe receipt not found in descriptors")
}
