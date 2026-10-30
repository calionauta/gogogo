package installer

// Strip tests: what a trim removes from the tree, per unit.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
)

func TestStripLandingBlock(t *testing.T) {
	dir := t.TempDir()
	routerDir := filepath.Join(dir, "internal", "server")
	if err := os.MkdirAll(routerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Flat buildChiMux shape: one route line per capability, no
	// unit-specific comments (guidance lives in SCOPE headers).
	give := "import (\n" +
		"\tlandinghandlers \"github.com/calionauta/gogogo/features/landing/handlers\"\n" +
		")\n" +
		"\tr.NotFound(landingH.Routes().ServeHTTP)\n" +
		"\n" +
		"\tr.MountFeature(\"config\", configH.Routes())\n"
	if err := os.WriteFile(filepath.Join(routerDir, "chi.go"), []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut("landing")
	drop := planTrim(keep)
	if len(drop) != 1 || drop[0].id != "landing" {
		t.Fatalf("expected only landing dropped, got %v", dropIDs(drop))
	}
	if err := applyTrim(dir, drop, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(routerDir, "chi.go"))
	if strings.Contains(string(out), `r.NotFound(landingH.Routes().ServeHTTP)`) {
		t.Errorf("landing route survived trim:\n%s", out)
	}
	if strings.Contains(string(out), `features/landing"`) {
		t.Errorf("landing import survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), `r.MountFeature("config", configH.Routes())`) {
		t.Errorf("config wiring was wrongly removed:\n%s", out)
	}
}

func TestStripOnboardingCallLine(t *testing.T) {
	// Flat BootChi shape: the DagNats.Enabled guard lives inside
	// bootOnboarding, so trimming is a single-line drop.
	dir := t.TempDir()
	routerDir := filepath.Join(dir, "internal", "server")
	if err := os.MkdirAll(routerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\t// Optional capabilities: one wiring line each.\n" +
		"\tonboardingH, onbShutdown := bootOnboarding(cfg, a, todoH, hub)\n" +
		"\tshutdown = append(shutdown, onbShutdown...)\n" +
		"\twbH := wbhandlers.NewHandler(queries, a, hub, persister, docs, cfg)\n"
	if err := os.WriteFile(filepath.Join(routerDir, "chi.go"), []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut("dagnats")
	drop := planTrim(keep)
	found := false
	for _, u := range drop {
		if u.id == "dagnats" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dagnats in drop set, got %v", dropIDs(drop))
	}
	if err := applyTrim(dir, drop, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(routerDir, "chi.go"))
	if strings.Contains(string(out), "bootOnboarding(cfg, a, todoH, hub)") {
		t.Errorf("onboarding wiring survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), "wbhandlers.NewHandler(") {
		t.Errorf("whiteboard wiring was wrongly removed:\n%s", out)
	}
	if !strings.Contains(string(out), "one wiring line each") {
		t.Errorf("shared convention comment was wrongly removed:\n%s", out)
	}
}

func TestDagnatsUnitCoversCoupledTests(t *testing.T) {
	// Regression guard for the e2e trim proof: these test files import
	// internal/dagnats from outside it, so the registry must own them or
	// `go build ./...` breaks after trim.
	byID := capabilities.ByID()
	c, ok := byID["dagnats"]
	if !ok {
		t.Fatal("dagnats capability missing from registry")
	}
	have := map[string]bool{}
	for _, f := range c.Files {
		have[f] = true
	}
	// A file under an owned directory goes with the unit without a per-file
	// entry: the trim deletes the whole directory.
	ownedDir := func(path string) bool {
		for _, d := range c.Dirs {
			if strings.HasPrefix(path, d+"/") {
				return true
			}
		}
		return false
	}
	for _, want := range []string{
		"features/onboarding/onboarding_test.go",
		"internal/nats/single_nats_test.go",
		"cmd/web/start_nats_test.go",
	} {
		if !have[want] && !ownedDir(want) {
			t.Errorf("dagnats capability does not own coupled test %q", want)
		}
	}
}

func TestManifestUnitsMatchRegistry(t *testing.T) { // Single source of truth, enforced: every installer unit maps to
	// registry capabilities, and every offered capability is reachable
	// from exactly one unit.
	byID := capabilities.ByID()
	covered := map[string]bool{}
	for _, u := range manifestUnits {
		if len(u.caps()) == 0 {
			t.Errorf("unit %q maps to no capability", u.id)
		}
		for _, id := range u.caps() {
			if _, ok := byID[id]; !ok {
				t.Errorf("unit %q maps to unknown capability %q", u.id, id)
			}
			covered[id] = true
		}
		m := u.meta()
		if m.kind != capabilities.KindPlugin && m.kind != capabilities.KindFeature {
			t.Errorf("unit %q resolves kind %q", u.id, m.kind)
		}
		if len(m.dirs)+len(m.files) == 0 {
			t.Errorf("unit %q owns no paths", u.id)
		}
	}
	for _, id := range capabilities.OfferedIDs() {
		if !covered[id] {
			t.Errorf("offered capability %q unreachable from any installer unit", id)
		}
	}
}

func TestReadmeDocumentsUnits(t *testing.T) {
	// Docs stay truthful: every installer unit id must appear backticked
	// in cmd/gogogo/README.md, or humans read about a unit that the
	// docs never explain.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable")
	}
	readme := filepath.Join(filepath.Dir(file), "..", "..", "cmd", "gogogo", "README.md")
	raw, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range manifestUnits {
		if !strings.Contains(string(raw), "`"+u.id+"`") {
			t.Errorf("README.md never mentions unit %q", u.id)
		}
	}
}

func TestWhiteboardBundleCoversCollab(t *testing.T) {
	// The bundle rule, encoded: whiteboard drops collab with it because
	// whiteboard-without-collab does not compile and collab-without-
	// whiteboard is dead. DependsOn documents the edge; the unit acts it.
	byID := capabilities.ByID()
	wb := byID["whiteboard"]
	found := false
	for _, dep := range wb.DependsOn {
		if dep == "collab" {
			found = true
		}
	}
	if !found {
		t.Errorf("whiteboard must declare DependsOn collab")
	}
	var wbUnit *trimUnit
	for i, u := range manifestUnits {
		if u.id == unitWhiteboard {
			wbUnit = &manifestUnits[i]
		}
	}
	if wbUnit == nil {
		t.Fatal("whiteboard unit missing")
	}
	hasCollab := false
	for _, id := range wbUnit.caps() {
		if id == "collab" {
			hasCollab = true
		}
	}
	if !hasCollab {
		t.Errorf("whiteboard unit must cover the collab capability")
	}
}

func TestApplyReceiptCountsStrips(t *testing.T) {
	// Receipts are the agent-visible proof of what changed: applied
	// strips counted, missed markers listed, re-runs quiet. Exercised
	// on the desktop Phase C block (brace-mode strip).
	dir := t.TempDir()
	desktopDir := filepath.Join(dir, "cmd", "desktop")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "import (\n\t\"context\"\n\t\"time\"\n\n" +
		"\t\"github.com/calionauta/gogogo/internal/collab\"\n)\n" +
		"\n" +
		"func main() {\n" +
		"\t_ = context.Background()\n" +
		"\t// Edge sync (Phase C): a demo publish keeps the edge path exercised.\n" +
		"\tdefer publishDemoCollab(nat, js)()\n" +
		"\n" +
		"\tviewURL := webviewURL(cfg.Host, port)\n" +
		"}\n" +
		"\n" +
		"// publishDemoCollab exercises the edge-sync publish paths end to end.\n" +
		"func publishDemoCollab(nat *nats.Handle, js nats.JetStreamLike) context.CancelFunc {\n" +
		"\tpub := collab.NewPublisher(nat.Conn)\n" +
		"\tt := time.NewTicker(time.Second)\n" +
		"\t_ = t\n" +
		"\treturn context.CancelFunc(func() {})\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(desktopDir, "main.go"), []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut(unitWhiteboard)
	rc := &Receipt{}
	if err := applyTrim(dir, planTrim(keep), rc); err != nil {
		t.Fatal(err)
	}
	var wb *UnitReceipt
	for i := range rc.Units {
		if rc.Units[i].ID == unitWhiteboard {
			wb = &rc.Units[i]
		}
	}
	if wb == nil {
		t.Fatalf("receipt = %+v", rc.Units)
	}
	if wb.StripsApplied != 2 {
		t.Errorf("StripsApplied = %d, want 2 (defer + function)", wb.StripsApplied)
	}
	if len(wb.StripsMissed) != 0 {
		t.Errorf("StripsMissed = %v, want none", wb.StripsMissed)
	}
	// Idempotent re-run: block already gone, marker missed, no error.
	rc2 := &Receipt{}
	if err := applyTrim(dir, planTrim(keep), rc2); err != nil {
		t.Fatal(err)
	}
	var wb2 *UnitReceipt
	for i := range rc2.Units {
		if rc2.Units[i].ID == unitWhiteboard {
			wb2 = &rc2.Units[i]
		}
	}
	if wb2 == nil {
		t.Fatalf("re-run receipt = %+v", rc2.Units)
	}
	if wb2.StripsApplied != 0 || len(wb2.StripsMissed) == 0 {
		t.Errorf("re-run receipt = %+v, want 0 applied + misses", wb2)
	}
}

func TestStripDagnatsMainKeepsStackWired(t *testing.T) {
	// On the flat stack the todo handler rides the booted stack (Serve and
	// AttachJetStream still use it), so the dagnats strip leaves no
	// dangling reference — nothing to blank. The contract is: engine calls
	// gone, the AttachJetStream line after them intact.
	dir := t.TempDir()
	webDir := filepath.Join(dir, "cmd", "web")
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\tdefer stopLifecycle()\n" +
		"\n" +
		"\t// DagNats owns the embedded NATS on :4222 and must boot before\n" +
		"\tstartDagNats(lifecycleCtx, cfg, stack.TodoHandler, stack.NotificationsHandler)\n" +
		"\tdefer shutdownDagNats()\n" +
		"\n" +
		"\tstack.AttachJetStream(lifecycleCtx, nc, js, cfg.NATS.LeafNodeURL != \"\")\n"
	if err := os.WriteFile(filepath.Join(webDir, "main.go"), []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut("dagnats")
	drop := planTrim(keep)
	if err := applyTrim(dir, drop, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(webDir, "main.go"))
	if strings.Contains(string(out), "startDagNats") || strings.Contains(string(out), "shutdownDagNats") {
		t.Errorf("dagnats boot calls survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), "stack.AttachJetStream(lifecycleCtx, nc, js, cfg.NATS.LeafNodeURL != \"\")") {
		t.Errorf("code after the strip was damaged:\n%s", out)
	}
}

func TestStripWhiteboardRemovesWiringAndDesktopPhaseC(t *testing.T) {
	// Flat BootChi/buildChiMux shape: trimming the whiteboard drops its
	// handler construction lines. The desktop Phase C demo (a deferred call
	// plus its function) must go too, or `go mod tidy` (which parses
	// cmd/desktop) fails on the orphaned import.
	dir := t.TempDir()
	routerDir := filepath.Join(dir, "internal", "server")
	desktopDir := filepath.Join(dir, "cmd", "desktop")
	if err := os.MkdirAll(routerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\t// Optional capabilities: one wiring line each.\n" +
		"\tonboardingH, onbShutdown := bootOnboarding(cfg, a, todoH, hub)\n" +
		"\twbhandlers.NewHandler(queries, a, hub, persister, docs, cfg),\n" +
		"\n" +
		"\tif cfg.OfflineSync.Enabled {\n"
	if err := os.WriteFile(filepath.Join(routerDir, "chi.go"), []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	dgive := "import (\n\t\"context\"\n\t\"time\"\n\n" +
		"\t\"github.com/calionauta/gogogo/internal/collab\"\n)\n" +
		"\n" +
		"func main() {\n" +
		"\t_ = context.Background()\n" +
		"\t// Edge sync (Phase C): a demo publish keeps the edge path exercised.\n" +
		"\tdefer publishDemoCollab(nat, js)()\n" +
		"\n" +
		"\tviewURL := webviewURL(cfg.Host, port)\n" +
		"}\n" +
		"\n" +
		"// publishDemoCollab exercises the edge-sync publish paths end to end.\n" +
		"func publishDemoCollab(nat *nats.Handle, js nats.JetStreamLike) context.CancelFunc {\n" +
		"\tpub := collab.NewPublisher(nat.Conn)\n" +
		"\tt := time.NewTicker(time.Second)\n" +
		"\t_ = t\n" +
		"\treturn context.CancelFunc(func() {})\n" +
		"}\n" +
		"\n" +
		"// desktopConfig loads config.\n" +
		"func desktopConfig() {}\n"
	if err := os.WriteFile(filepath.Join(desktopDir, "main.go"), []byte(dgive), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut(unitWhiteboard)
	drop := planTrim(keep)
	if err := applyTrim(dir, drop, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(routerDir, "chi.go"))
	if strings.Contains(string(out), "wbhandlers.NewHandler") || strings.Contains(string(out), "internal/collab") {
		t.Errorf("whiteboard wiring survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), "bootOnboarding(cfg, a, todoH, hub)") {
		t.Errorf("onboarding wiring was wrongly removed:\n%s", out)
	}
	dout, _ := os.ReadFile(filepath.Join(desktopDir, "main.go"))
	for _, want := range []string{"collab", `"time"`} {
		if strings.Contains(string(dout), want) {
			t.Errorf("desktop still references %q after trim:\n%s", want, dout)
		}
	}
	if !strings.Contains(string(dout), `"context"`) {
		t.Errorf("desktop lost the context import still used outside the demo:\n%s", dout)
	}
	if !strings.Contains(string(dout), "viewURL :=") {
		t.Errorf("desktop lost code after the strip:\n%s", dout)
	}
}

// TestTrimFailsLoudOnGoModReadError closes the silent-no-op class: a read
// error that is not "file absent" (here go.mod is a DIRECTORY, so ReadFile
// fails with EISDIR) must fail the trim. Swallowing it left the tree
// half-trimmed with a receipt claiming the unit was handled.
//
// Red-proof: restore `_ = dropGoModRequire(...)` in applyExtraFiles and this
// test fails — applyTrim returns nil.
func TestTrimFailsLoudOnGoModReadError(t *testing.T) {
	var u trimUnit
	for _, cand := range manifestUnits {
		if len(cand.goModDrops) > 0 {
			u = cand
			break
		}
	}
	if u.id == "" {
		t.Skip("no unit drops go.mod requires any more — nothing to guard")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "go.mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := applyTrim(root, []trimUnit{u}, nil); err == nil {
		t.Fatalf("applyTrim swallowed an unreadable go.mod for unit %s", u.id)
	}
}
