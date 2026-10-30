package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolsListCapabilities(t *testing.T) {
	res, _, err := handleCapabilitiesList(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(t, res)
	var caps []map[string]any
	if err := json.Unmarshal([]byte(text), &caps); err != nil {
		t.Fatalf("capabilities_list is not valid JSON: %v", err)
	}
	if len(caps) == 0 {
		t.Fatal("empty capability list")
	}
	found := map[string]bool{}
	for _, c := range caps {
		if id, ok := c["id"].(string); ok {
			found[id] = true
		}
		if _, ok := c["kind"]; !ok {
			t.Errorf("capability missing kind: %v", c)
		}
	}
	for _, want := range []string{"dagnats", "whiteboard", "nats", "todo", "shell"} {
		if !found[want] {
			t.Errorf("capability %q missing from list", want)
		}
	}
}

func TestTrimApplyRequiresConfirm(t *testing.T) {
	_, _, err := handleTrimApply(context.Background(), nil, trimArgs{Name: "x", Dir: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "confirm:true") {
		t.Errorf("expected confirm:true refusal, got %v", err)
	}
}

func TestAddUnitRequiresConfirm(t *testing.T) {
	_, _, err := handleAddUnit(context.Background(), nil,
		addArgs{Unit: "landing", From: "/tmp", Dir: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "confirm:true") {
		t.Errorf("expected confirm:true refusal, got %v", err)
	}
}

func TestAddUnitValidatesFields(t *testing.T) {
	_, _, err := handleAddUnit(context.Background(), nil,
		addArgs{Unit: "landing", Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected required-fields error, got %v", err)
	}
	_, _, err = handleAddUnit(context.Background(), nil,
		addArgs{Unit: "nats", From: "/tmp", Dir: "/tmp", Confirm: true})
	if err == nil {
		t.Errorf("expected error for non-offered unit nats")
	}
}

func TestCheckTreeRequiresDir(t *testing.T) {
	_, _, err := handleCheckTree(context.Background(), nil, dirArgs{})
	if err == nil {
		t.Errorf("expected dir-required error")
	}
}

func TestAdviseStackNeedsNothing(t *testing.T) { // The guidance tool is read-only: no dir, no confirm, no changes.
	res, _, err := handleAdvise(context.Background(), nil, adviseArgs{Need: "offline airplane mode"})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(t, res)
	var doc struct {
		Presets []struct {
			Name string `json:"name"`
		} `json:"presets"`
	}
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("advise_stack is not valid JSON: %v", err)
	}
	if len(doc.Presets) == 0 || doc.Presets[0].Name != "offline-first" {
		t.Errorf("offline need did not surface offline-first: %v", doc.Presets)
	}
}

func TestAdviseStackGenericWordsStayInGoScopes(t *testing.T) {
	// Over the MCP transport, like an agent would call it: generic English
	// words must never route outside Go scopes, and every answer must be
	// valid JSON with a scope the docs promise.
	for _, need := range []string{
		"native serialization fast path behind a C ABI, with a pure-Go fallback",
		"fast",
		"Zig kernel",
		"Django blog",
	} {
		res, _, err := handleAdvise(context.Background(), nil, adviseArgs{Need: need})
		if err != nil {
			t.Fatalf("%q: %v", need, err)
		}
		var doc struct {
			Scope string `json:"scope"`
		}
		if err := json.Unmarshal([]byte(toolText(t, res)), &doc); err != nil {
			t.Fatalf("%q is not valid JSON: %v", need, err)
		}
		switch doc.Scope {
		case "template", "go-standards", "exception-to-go":
		default:
			t.Errorf("%q → scope %q, want template/go-standards/exception-to-go", need, doc.Scope)
		}
	}
}

func TestProtocolListsCatalogTools(t *testing.T) {
	// End-to-end over in-memory transports: the served surface is
	// exactly the catalog, callable through the MCP protocol.
	ctx := context.Background()
	server := buildServer()
	clientTrans, serverTrans := mcp.NewInMemoryTransports()
	// Canonical SDK order: server connects first (synchronously), then
	// the client (see modelcontextprotocol/go-sdk server_example_test).
	if _, err := server.Connect(ctx, serverTrans, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client"}, nil)
	session, err := client.Connect(ctx, clientTrans, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != len(toolCatalog) {
		names := []string{}
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
		}
		t.Fatalf("want %d tools, got %d: %v", len(toolCatalog), len(res.Tools), names)
	}
	call, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "capabilities_list"})
	if err != nil {
		t.Fatal(err)
	}
	if len(call.Content) == 0 {
		t.Fatal("empty call result")
	}
}

func toolText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("empty tool result")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("non-text content: %T", res.Content[0])
	}
	return tc.Text
}

func TestTrimApplyEndToEnd(t *testing.T) {
	// The mutating path for real: smallest compilable template tree in,
	// rename + REAL tidy/build proof out, machine-readable envelope back.
	// Breaks when arg mapping, apply, rename, or proof regresses.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module github.com/calionauta/gogogo\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "web", "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, _, err := handleTrimApply(context.Background(), nil, trimArgs{
		Name: "mcp-app", Owner: "mcporg", Dir: dir, Confirm: true, Format: "json",
	})
	if err != nil {
		t.Fatalf("trim_apply: %v", err)
	}
	var env struct {
		BuildOk bool `json:"buildOk"`
		Plan    struct {
			Module string `json:"module"`
		} `json:"plan"`
		Next struct {
			Dir string `json:"dir"`
		} `json:"next"`
	}
	if uerr := json.Unmarshal([]byte(toolText(t, res)), &env); uerr != nil {
		t.Fatalf("trim_apply is not valid JSON: %v", uerr)
	}
	if !env.BuildOk {
		t.Error("buildOk must be true for the compilable fixture")
	}
	if env.Plan.Module != "github.com/mcporg/mcp-app" {
		t.Errorf("module = %q, want the renamed path", env.Plan.Module)
	}
	if env.Next.Dir != dir {
		t.Errorf("next.dir = %q, want %q", env.Next.Dir, dir)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "module github.com/mcporg/mcp-app") {
		t.Errorf("go.mod was not renamed on disk:\n%s", raw)
	}
}

func TestMcpReadmeListsTools(t *testing.T) {
	// Docs stay truthful: every served tool must appear backticked in the
	// module README's tools table, or humans read about a surface the
	// docs never explain.
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range toolCatalog {
		if !strings.Contains(string(raw), "`"+d.name+"`") {
			t.Errorf("README.md never mentions tool %q", d.name)
		}
	}
}

// writeNewFeatureFixture builds the smallest checkout `new` accepts: the two
// anchors it edits, an empty registry, and a policy file. Dry-run never
// builds, so the fixture needs no compilable packages.
func writeNewFeatureFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"go.mod": "module example.com/demo\n\ngo 1.27\n",
		"internal/server/chi.go": `package server

import (
	confighandlers "example.com/demo/features/config/handlers"
)

func boot() {
	mux := buildChiMux()
	mountConsoleProxy(mux, a, cfg)
	mux.HandleFunc("/api/stream/status", StreamStatusHandler(hub))
	_ = mux
	_ = confighandlers.NewHandler
}

func buildChiMux() *int { return new(int) }
`,
		"internal/capabilities/registry.go": "package capabilities\n\nvar All = []Capability{\n}\n",
		"internal/rbac/casbin/policy.csv":   "p, admin, /*, .*\n",
	}
	for rel, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestNewFeatureDryRunPlansScaffold proves the tool reaches the same engine as
// the CLI: a dry run returns the plan and touches nothing.
func TestNewFeatureDryRunPlansScaffold(t *testing.T) {
	dir := t.TempDir()
	writeNewFeatureFixture(t, dir)
	res, _, err := handleNewFeature(context.Background(), nil,
		newFeatureArgs{Name: "reports", Dir: dir, DryRun: true, Format: "json"})
	if err != nil {
		t.Fatalf("handleNewFeature: %v", err)
	}
	var plan map[string]any
	if err := json.Unmarshal([]byte(toolText(t, res)), &plan); err != nil {
		t.Fatalf("dry-run is not one JSON document: %v", err)
	}
	for _, key := range []string{"creates", "edits", "proof", "next"} {
		if _, ok := plan[key]; !ok {
			t.Errorf("plan missing %q: %v", key, plan)
		}
	}
	if _, statErr := os.Stat(filepath.Join(dir, "features", "reports")); !os.IsNotExist(statErr) {
		t.Error("dry run wrote files — it must change nothing")
	}
}

// TestNewFeatureRequiresConfirm keeps the destructive-tool contract: no
// confirm and no dryRun is a refusal, not a scaffold.
func TestNewFeatureRequiresConfirm(t *testing.T) {
	_, _, err := handleNewFeature(context.Background(), nil,
		newFeatureArgs{Name: "reports", Dir: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "confirm:true") {
		t.Errorf("expected confirm:true refusal, got %v", err)
	}
	_, _, err = handleNewFeature(context.Background(), nil, newFeatureArgs{Name: "reports", Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("expected required-fields error, got %v", err)
	}
}

// TestMigratePlanDeliversTheList is the transport half of `gogogo migrate`:
// same engine, so the tool only has to pass the arguments through. Uses a local
// changelog so the test never depends on the network.
func TestMigratePlanDeliversTheList(t *testing.T) {
	dir := t.TempDir()
	entries := "## 0.53.0 (2026-10-21)\n\n* feat(ui): shadcn skin by @a in https://example/199\n" +
		"\n## 0.52.0 (2026-10-20)\n\n* fix(server): origin guard by @a in https://example/203\n"
	changelog := filepath.Join(dir, "CHANGELOG.md")
	if err := os.WriteFile(changelog, []byte(entries), 0o600); err != nil {
		t.Fatal(err)
	}
	res, _, err := handleMigratePlan(context.Background(), nil, migrateArgs{
		Dir: dir, From: "v0.51.0", To: "v0.53.0", Changelog: changelog,
	})
	if err != nil {
		t.Fatalf("handleMigratePlan: %v", err)
	}
	var rep struct {
		From     string `json:"from"`
		To       string `json:"to"`
		Releases []struct {
			Version string   `json:"version"`
			Entries []string `json:"entries"`
		} `json:"releases"`
		Gates []string `json:"gates"`
	}
	if jerr := json.Unmarshal([]byte(toolText(t, res)), &rep); jerr != nil {
		t.Fatalf("migrate_plan must answer JSON: %v", jerr)
	}
	if len(rep.Releases) != 2 || rep.Releases[0].Version != "0.53.0" {
		t.Errorf("releases = %+v", rep.Releases)
	}
	if len(rep.Gates) == 0 {
		t.Error("the plan must carry the proof gates")
	}
}

// TestVersionStatusIsAVerdictNotAnError pins the one behavior that differs from
// every other tool: "an update exists" arrives as exit 3, and the call must
// still deliver its JSON instead of turning into a tool error. An agent that
// gets an error stops reading the answer it asked for.
func TestVersionStatusIsAVerdictNotAnError(t *testing.T) {
	dir := t.TempDir()
	body := "# AGENTS\n\n## Trim provenance\n\nscaffolded-from: calionauta/gogogo@v0.1.0\n" +
		"template-commit: unknown\nremoved: none\nremoved-paths: none\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Keep the check offline and deterministic: a stale cache entry is a
	// verdict we can predict without the network.
	cacheRoot := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheRoot)
	if err := os.MkdirAll(filepath.Join(cacheRoot, "gogogo"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The cache entry is keyed by repo: a template entry must never answer a
	// fork's lookup, so writing one without the repo field is a miss.
	entry := `{"repo":"calionauta/gogogo","tag":"v999.0.0","checkedAt":"` +
		time.Now().UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(filepath.Join(cacheRoot, "gogogo", "update-check.json"),
		[]byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}

	res, _, err := handleVersionStatus(context.Background(), nil, versionArgs{Dir: dir})
	if err != nil {
		t.Fatalf("a verdict must not surface as a tool error: %v", err)
	}
	text := toolText(t, res)
	var rep struct {
		Installed  string `json:"installed"`
		Latest     string `json:"latest"`
		TreeStatus string `json:"treeStatus"`
	}
	if jerr := json.Unmarshal([]byte(text), &rep); jerr != nil {
		t.Fatalf("version_status must answer JSON: %v\n%s", jerr, text)
	}
	if rep.TreeStatus != "behind" {
		t.Errorf("treeStatus = %q, want behind (base v0.1.0, latest v999.0.0)", rep.TreeStatus)
	}
	if rep.Latest != "v999.0.0" {
		t.Errorf("latest = %q, want the cached tag", rep.Latest)
	}
}

// TestAddUnitDryRunReachesTheEngine is the plumbing test that was missing.
// add_unit used to pass --no-tui to `gogogo add`, which never defined that flag
// (it belongs to the root scaffolder), so every call failed at flag parse —
// and nothing noticed, because this module's tests ran in no gate. A dry run
// against the template itself proves the argv reaches the real engine without
// changing anything.
func TestAddUnitDryRunReachesTheEngine(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	res, _, err := handleAddUnit(context.Background(), nil, addArgs{
		Unit: "whiteboard", From: root, Dir: root, DryRun: true, Format: "json",
	})
	if err != nil {
		t.Fatalf("handleAddUnit: %v", err)
	}
	if out := toolText(t, res); !strings.Contains(out, "whiteboard") {
		t.Errorf("dry-run plan does not mention the unit: %s", out)
	}
}

// TestExportUnitWritesOnlyTheScratchDirectory proves export_unit is wired to
// the same engine and that its read-only promise survives the transport: the
// bundle lands in `to`, and `dir` is untouched.
func TestExportUnitWritesOnlyTheScratchDirectory(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "bundle")
	res, _, err := handleExportUnit(context.Background(), nil, exportArgs{
		Unit: "sounds", Dir: root, To: to, Format: "json",
	})
	if err != nil {
		t.Fatalf("handleExportUnit: %v", err)
	}
	var bundle map[string]any
	if err := json.Unmarshal([]byte(toolText(t, res)), &bundle); err != nil {
		t.Fatalf("export is not one JSON document: %v", err)
	}
	if bundle["unit"] != "sounds" {
		t.Errorf("unit = %v, want sounds", bundle["unit"])
	}
	if _, statErr := os.Stat(filepath.Join(to, "PORTING.md")); statErr != nil {
		t.Errorf("recipe missing: %v", statErr)
	}
}

// TestCapabilitiesListExposesScaffold pins the agent-facing schema: the class
// replaces the old offered boolean, and every capability carries it.
func TestCapabilitiesListExposesScaffold(t *testing.T) {
	res, _, err := handleCapabilitiesList(context.Background(), nil, struct{}{})
	if err != nil {
		t.Fatalf("capabilities_list: %v", err)
	}
	var caps []struct {
		ID       string `json:"id"`
		Scaffold string `json:"scaffold"`
		Offered  *bool  `json:"offered"`
	}
	if jerr := json.Unmarshal([]byte(toolText(t, res)), &caps); jerr != nil {
		t.Fatalf("capabilities_list is not JSON: %v", jerr)
	}
	perf, auth := false, false
	for _, c := range caps {
		if c.Offered != nil {
			t.Errorf("%s still exposes the removed offered field", c.ID)
		}
		switch c.ID {
		case "perf":
			perf = true
			if c.Scaffold != "opt-in" {
				t.Errorf("perf scaffold = %q, want opt-in", c.Scaffold)
			}
		case "auth":
			auth = true
			if c.Scaffold != "core" {
				t.Errorf("auth scaffold = %q, want core", c.Scaffold)
			}
		}
	}
	if !perf || !auth {
		t.Errorf("missing perf/auth in capabilities_list (perf=%v auth=%v)", perf, auth)
	}
}
