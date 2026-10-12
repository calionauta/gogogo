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
		"\tlandingchi \"github.com/calionauta/gogogo/features/landing/chi\"\n" +
		")\n" +
		"\tmux.Handle(\"/\", landingH.Routes())\n" +
		"\n" +
		"\tmux.Handle(\"/config\", configH.Routes())\n"
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
	if strings.Contains(string(out), `mux.Handle("/", landingH.Routes())`) {
		t.Errorf("landing route survived trim:\n%s", out)
	}
	if strings.Contains(string(out), `features/landing"`) {
		t.Errorf("landing import survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), `mux.Handle("/config", configH.Routes())`) {
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
		"\twbH := wbchi.NewHandler(queries, a, hub, persister, docs, cfg)\n"
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
	if !strings.Contains(string(out), "wbchi.NewHandler(") {
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
	for _, want := range []string{
		"features/todo/chi/onboarding.go",
		"features/todo/chi/onboarding_test.go",
		"internal/nats/single_nats_test.go",
		"cmd/web/start_nats_test.go",
	} {
		if !have[want] {
			t.Errorf("dagnats capability does not own coupled test %q", want)
		}
	}
}

func TestManifestUnitsMatchRegistry(t *testing.T) { // Single source of truth, enforced: every installer unit maps to
	// registry capabilities, and every offered capability is reachable
	// from exactly one unit (skins-extra reaches skins).
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
	give := "\t// Edge sync (Phase C): publish local Loro updates on app.sync.<docID>.\n" +
		"\tif js != nil {\n" +
		"\t\tpub := collab.NewPublisher(nats.Conn())\n" +
		"\t}\n" +
		"\n" +
		"\taddr := fmt.Sprintf(\"%s:%d\", cfg.Host, cfg.Port)\n"
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
	if wb.StripsApplied != 1 {
		t.Errorf("StripsApplied = %d, want 1", wb.StripsApplied)
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
	give := "\tdefer shutdown()\n" +
		"\n" +
		"\t// DagNats owns the embedded NATS on :4222 and must boot before\n" +
		"\tstartDagNats(lifecycleCtx, cfg, stack.TodoHandler)\n" +
		"\tdefer shutdownDagNats()\n" +
		"\n" +
		"\tstack.AttachJetStream(lifecycleCtx, js)\n"
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
	if !strings.Contains(string(out), "stack.AttachJetStream(lifecycleCtx, js)") {
		t.Errorf("code after the strip was damaged:\n%s", out)
	}
}

func TestStripWhiteboardRemovesWiringAndDesktopPhaseC(t *testing.T) {
	// Flat BootChi/buildChiMux shape: trimming the whiteboard drops its
	// handler construction lines. The desktop Phase C demo (brace-mode
	// block) must go too, or `go mod tidy` (which parses cmd/desktop)
	// fails on the orphaned import.
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
		"\twbchi.NewHandler(queries, a, hub, persister, docs, cfg),\n" +
		"\n" +
		"\tif cfg.OfflineSync.Enabled {\n"
	if err := os.WriteFile(filepath.Join(routerDir, "chi.go"), []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	dgive := "import (\n\t\"context\"\n\t\"time\"\n\n" +
		"\t\"github.com/calionauta/gogogo/internal/collab\"\n)\n" +
		"\n" +
		"\t// Edge sync (Phase C): publish local Loro updates on app.sync.<docID>.\n" +
		"\tif js != nil && nats.Conn() != nil {\n" +
		"\t\tpub := collab.NewPublisher(nats.Conn())\n" +
		"\t}\n" +
		"\n" +
		"\taddr := fmt.Sprintf(\"%s:%d\", cfg.Host, cfg.Port)\n"
	if err := os.WriteFile(filepath.Join(desktopDir, "main.go"), []byte(dgive), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut(unitWhiteboard)
	drop := planTrim(keep)
	if err := applyTrim(dir, drop, nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(routerDir, "chi.go"))
	if strings.Contains(string(out), "wbchi.NewHandler") || strings.Contains(string(out), "internal/collab") {
		t.Errorf("whiteboard wiring survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), "bootOnboarding(cfg, a, todoH, hub)") {
		t.Errorf("onboarding wiring was wrongly removed:\n%s", out)
	}
	dout, _ := os.ReadFile(filepath.Join(desktopDir, "main.go"))
	for _, want := range []string{"collab", `"context"`, `"time"`} {
		if strings.Contains(string(dout), want) {
			t.Errorf("desktop still references %q after trim:\n%s", want, dout)
		}
	}
	if !strings.Contains(string(dout), "addr :=") {
		t.Errorf("desktop lost code after the strip:\n%s", dout)
	}
}
