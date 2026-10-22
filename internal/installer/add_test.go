package installer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportSlot(t *testing.T) {
	lines := []string{
		"import (",
		"\t\"context\"",
		"\t\"fmt\"",
		"",
		"\t\"github.com/calionauta/gogogo/features/todo\"",
		")",
	}
	if got := importSlot(lines, "\t\"github.com/calionauta/gogogo/features/landing\""); got != 4 {
		t.Errorf("landing import slot = %d, want 4 (sorted in external group)", got)
	}
	if got := importSlot(lines, "\t\"errors\""); got != 2 {
		t.Errorf("errors import slot = %d, want 2 (sorted in stdlib group)", got)
	}
	if got := importSlot([]string{"package x"}, "\t\"fmt\""); got != -1 {
		t.Errorf("missing import block slot = %d, want -1", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// mustOpenTree opens a treeFS for a test and closes it on cleanup.
func mustOpenTree(t *testing.T, root string) *treeFS {
	t.Helper()
	tree, err := openTree(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tree.Close() })
	return tree
}

// fixtureAddTrees builds a pristine template tree (from) and a trimmed
// project tree (proj) for the landing unit: template has the package +
// wiring, project has neither.
func fixtureAddTrees(t *testing.T) (from, proj string) {
	t.Helper()
	from = t.TempDir()
	proj = t.TempDir()
	writeFile(t, filepath.Join(from, "features", "landing", "landing.go"), "package landing\n")
	writeFile(t, filepath.Join(proj, "go.mod"), "module example.com/proj\n\ngo 1.27\n")
	chiFull := "import (\n" +
		"\t\"github.com/calionauta/gogogo/features/config\"\n" +
		"\tlandinghandlers \"github.com/calionauta/gogogo/features/landing/handlers\"\n" +
		")\n" +
		"\tr.MountFeature(\"config\", configH.Routes())\n" +
		"\tr.NotFound(landingH.Routes().ServeHTTP)\n" +
		"\treturn r\n"
	writeFile(t, filepath.Join(from, "internal", "server", "chi.go"), chiFull)
	chiTrimmed := "import (\n" +
		"\t\"github.com/calionauta/gogogo/features/config\"\n" +
		")\n" +
		"\tr.MountFeature(\"config\", configH.Routes())\n" +
		"\treturn r\n"
	writeFile(t, filepath.Join(proj, "internal", "server", "chi.go"), chiTrimmed)
	writeFile(t, filepath.Join(from, "go.mod"), "module example.com/tpl\n\ngo 1.27\n")
	return from, proj
}

func findUnit(t *testing.T, id string) trimUnit {
	t.Helper()
	for _, u := range manifestUnits {
		if u.id == id {
			return u
		}
	}
	t.Fatalf("unit %q missing", id)
	return trimUnit{}
}

// TestUnitPathsPresentPinsIdempotentReadd pins the gate's skip: a unit
// whose owned paths all exist restores nothing, so add skips both the
// copy and the reversibility refusal (re-adding what is there is a no-op,
// not an error).
func TestUnitPathsPresentPinsIdempotentReadd(t *testing.T) {
	dir := t.TempDir()
	u := findUnit(t, unitSounds)
	if unitPathsPresent(dir, u) {
		t.Fatal("empty dir reports sounds present")
	}
	for _, d := range u.meta().dirs {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range u.meta().files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(f))), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(f)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if !unitPathsPresent(dir, u) {
		t.Fatal("complete dirs+files report sounds missing")
	}
	os.Remove(filepath.Join(dir, filepath.FromSlash(u.meta().files[0])))
	if unitPathsPresent(dir, u) {
		t.Fatal("one file missing still reports present")
	}
}

func TestAddUnitRestoresLanding(t *testing.T) {
	from, proj := fixtureAddTrees(t)
	rc := &AddReceipt{ID: unitLanding, Warnings: []string{}}
	if err := addUnit(from, proj, findUnit(t, unitLanding), rc); err != nil {
		t.Fatal(err)
	}
	if rc.DirsCopied != 1 {
		t.Errorf("DirsCopied = %d, want 1", rc.DirsCopied)
	}
	raw, _ := os.ReadFile(filepath.Join(proj, "internal", "server", "chi.go"))
	out := string(raw)
	if !strings.Contains(out, `r.NotFound(landingH.Routes().ServeHTTP)`) {
		t.Errorf("route line not restored:\n%s", out)
	}
	if !strings.Contains(out, `landinghandlers "github.com/calionauta/gogogo/features/landing/handlers"`) {
		t.Errorf("import not restored:\n%s", out)
	}
	// Import lands sorted: config before landing.
	if strings.Index(out, "features/config") > strings.Index(out, "features/landing") {
		t.Errorf("imports not sorted:\n%s", out)
	}
	// Idempotent re-add: nothing more to do.
	rc2 := &AddReceipt{ID: unitLanding, Warnings: []string{}}
	if err := addUnit(from, proj, findUnit(t, unitLanding), rc2); err != nil {
		t.Fatal(err)
	}
	if rc2.DirsCopied+rc2.FilesCopied+rc2.LinesInserted != 0 {
		t.Errorf("re-add not idempotent: %+v", rc2)
	}
}

func TestAddUnitRefusesForeignTree(t *testing.T) {
	from, _ := fixtureAddTrees(t)
	proj := t.TempDir() // no internal/server/chi.go, no go.mod
	rc := &AddReceipt{}
	if err := addUnit(from, proj, findUnit(t, unitLanding), rc); err == nil {
		t.Fatal("expected refusal on non-scaffolded checkout")
	}
}

func TestAddUnknownUnitFailsFast(t *testing.T) {
	err := Run(context.Background(), []string{"add", "nats", "--from", "/tmp", "--dir", "/tmp"}, devNull(t), devNull(t))
	if err == nil {
		t.Fatal("expected error for non-offered unit nats")
	}
	if !strings.Contains(err.Error(), "dagnats") {
		t.Errorf("error must list valid units: %v", err)
	}
}

func TestAddDagnatsRestoresBootOrder(t *testing.T) {
	// Order is load-bearing: the engine must boot before startNATS, which
	// then connects to the NATS instance the engine started. The add must
	// reproduce source order, not anchor order.
	//
	// The fixture mirrors cmd/web/main.go's REAL anchors
	// (mainCallsAnchor="defer stopLifecycle()", the shutdown comment)
	// instead of invented markers, so a rename in the real file fails
	// here rather than silently breaking `add` — which is exactly what
	// the previous fixture masked.
	from := t.TempDir()
	proj := t.TempDir()
	src := "\tdefer stopLifecycle()\n" +
		"\n" +
		"\tstartDagNats(lifecycleCtx, cfg, stack.TodoHandler)\n" +
		"\tdefer shutdownDagNats()\n" +
		"\n" +
		"\tjs := startNATS(cfg)\n" +
		"\n" +
		"\t// The process is the server now (there is no framework run loop\n"
	writeFile(t, filepath.Join(from, "cmd", "web", "main.go"), src)
	dst := "\tdefer stopLifecycle()\n" +
		"\n" +
		"\tjs := startNATS(cfg)\n" +
		"\n" +
		"\t// The process is the server now (there is no framework run loop\n"
	writeFile(t, filepath.Join(proj, "cmd", "web", "main.go"), dst)
	u := findUnit(t, unitDagnats)
	tree := mustOpenTree(t, proj)
	for _, r := range u.mainStrips {
		anchor := anchorAfter(mainCallsAnchor)
		if r.addBefore != "" || r.addAfter != "" {
			anchor = spanAnchor{before: r.addBefore, after: r.addAfter}
		}
		if _, err := insertSpan(tree, from, proj,
			filepath.Join("cmd", "web", "main.go"),
			filepath.Join("cmd", "web", "main.go"),
			[]stripRule{r}, anchor, &AddReceipt{}); err != nil {
			t.Fatal(err)
		}
	}
	out, _ := os.ReadFile(filepath.Join(proj, "cmd", "web", "main.go"))
	text := string(out)
	boot := strings.Index(text, "startDagNats(lifecycleCtx, cfg, stack.TodoHandler)")
	nats := strings.Index(text, "js := startNATS(cfg)")
	server := strings.Index(text, "// The process is the server now")
	if boot == -1 || nats == -1 || server == -1 {
		t.Fatalf("re-insertion incomplete:\n%s", text)
	}
	ordered := boot < nats && nats < server
	if !ordered {
		t.Errorf("boot order violated (want boot < nats < server):\n%s", text)
	}
}

func TestRebaseModulePrefix(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/proj\n\ngo 1.27\n")
	writeFile(t, filepath.Join(dir, "x", "x.go"),
		"package x\n\nimport \"github.com/calionauta/gogogo/internal/queue\"\n")
	// Rebase only the x dir by faking a unit scope is overkill; call the
	// helper over a synthetic tree instead.
	tree := mustOpenTree(t, dir)
	n, err := rebaseTree(tree, filepath.Join(dir, "x"), "github.com/calionauta/gogogo", "example.com/proj")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("rebaseTree rewrote %d files, want 1", n)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "x", "x.go"))
	if !strings.Contains(string(raw), "example.com/proj/internal/queue") {
		t.Errorf("prefix not rebased:\n%s", raw)
	}
}

func TestMergeSingleImports(t *testing.T) {
	// .templ headers use lone `import "x"` lines: merging must produce a
	// sorted block (goimports-clean), not stacked single lines.
	lines := []string{"package auth", "", `import "b/example"`, "", "// Navbar"}
	out, err := mergeSingleImports(lines, `import "a/example"`)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(out, "\n")
	for _, want := range []string{"import (", "\t\"a/example\"", "\t\"b/example\"", ")"} {
		if !strings.Contains(joined, want) {
			t.Errorf("merged block missing %q:\n%s", want, joined)
		}
	}
	// No imports yet: lone line after the package clause.
	out2, err := mergeSingleImports([]string{"package auth", "", "// Navbar"}, `import "a/example"`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(out2, "\n"), `import "a/example"`) {
		t.Errorf("lone import not added:\n%s", strings.Join(out2, "\n"))
	}
}

func TestImportRestorationFollowsUsage(t *testing.T) {
	// Restoring an import without its usage breaks the build (unused
	// import); skipping it when the usage is gone is correct.
	withUse := []string{
		"import (",
		"\t\"fmt\"",
		")",
		"",
		"\tlanding.New(cfg).RegisterRoutes(se)",
	}
	if !importUsed(withUse, "\t\"github.com/x/features/landing\"") {
		t.Errorf("import with usage must be restored")
	}
	without := []string{
		"import (",
		"\t\"fmt\"",
		")",
	}
	if importUsed(without, "\t\"github.com/x/features/sounds\"") {
		t.Errorf("import without usage must be skipped")
	}
	// Blank side-effect imports are always restored (usage is the import).
	blank := []string{"package components", ""}
	if !importUsed(blank, "\t_ \"github.com/x/web/skins/daisyui\"") {
		t.Errorf("blank import must always be restored")
	}
}

// TestAddRefusesToolchainMismatch pins the gate `add` shares with the trim
// path: the tree asks for a go version GOTOOLCHAIN pins away, so the rewire
// would run and `go mod tidy` would fail afterwards. Refuse first, change
// nothing.
//
// Red-proof: drop the checkToolchainForTree call from executeAdd and the add
// proceeds (chi.go rewritten) — this test then fails on the refusal assertion.
func TestAddRefusesToolchainMismatch(t *testing.T) {
	from, proj := fixtureAddTrees(t)
	writeGoMod(t, proj, "1.27.3")
	chiPath := filepath.Join(proj, "internal", "server", "chi.go")
	before, err := os.ReadFile(chiPath)
	if err != nil {
		t.Fatal(err)
	}
	stubToolchain(t, "go1.27.2", "go1.27.2")

	runErr := Run(context.Background(), []string{
		"add", unitLanding, "--from", from, "--dir", proj, "--yes",
	}, devNull(t), devNull(t))
	if runErr == nil || !strings.Contains(runErr.Error(), "needs go1.27.3") {
		t.Fatalf("add must refuse a tree this toolchain cannot build, got %v", runErr)
	}
	after, readErr := os.ReadFile(chiPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(before) {
		t.Error("the refused add rewired chi.go anyway")
	}
}
