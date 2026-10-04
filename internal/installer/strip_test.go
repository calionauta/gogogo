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
	routerDir := filepath.Join(dir, "router")
	if err := os.MkdirAll(routerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Phase 2 Init shape: one bare call line per capability, no
	// unit-specific comments (guidance lives in SCOPE headers).
	give := "import (\n" +
		"\t\"github.com/calionauta/gogogo/features/landing\"\n" +
		")\n" +
		"\t\tlanding.New(cfg).RegisterRoutes(se)\n" +
		"\n" +
		"\t\tcfgfeature.New(cfg).RegisterRoutes(se)\n"
	if err := os.WriteFile(filepath.Join(routerDir, "router.go"), []byte(give), 0o600); err != nil {
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
	out, _ := os.ReadFile(filepath.Join(routerDir, "router.go"))
	if strings.Contains(string(out), "landing.New") {
		t.Errorf("landing wiring survived trim:\n%s", out)
	}
	if strings.Contains(string(out), `features/landing"`) {
		t.Errorf("landing import survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), "cfgfeature.New") {
		t.Errorf("config wiring was wrongly removed:\n%s", out)
	}
}

func TestStripOnboardingCallLine(t *testing.T) {
	// Phase 2 Init shape: the DagNats.Enabled guard moved inside
	// registerOnboarding, so trimming is a single-line drop.
	dir := t.TempDir()
	routerDir := filepath.Join(dir, "router")
	if err := os.MkdirAll(routerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\t\t// Optional capabilities: one call each.\n" +
		"\t\tregisterOnboarding(app, q, se, broadcaster, todoH, cfg)\n" +
		"\t\tregisterWhiteboardStack(se, q, cfg)\n"
	if err := os.WriteFile(filepath.Join(routerDir, "router.go"), []byte(give), 0o600); err != nil {
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
	out, _ := os.ReadFile(filepath.Join(routerDir, "router.go"))
	if strings.Contains(string(out), "registerOnboarding") {
		t.Errorf("onboarding wiring survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), "registerWhiteboardStack") {
		t.Errorf("whiteboard wiring was wrongly removed:\n%s", out)
	}
	if !strings.Contains(string(out), "one call each") {
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
		"router/export_test.go",
		"features/todo/handlers/onboarding_resume_test.go",
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

func TestStripDagnatsMainKeepsTodoHWired(t *testing.T) {
	// startDagNats is the only todoH use in cmd/web/main.go; after the
	// strip the installer must leave a blank use or the build fails with
	// "declared and not used: todoH".
	dir := t.TempDir()
	webDir := filepath.Join(dir, "cmd", "web")
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\tdefer shutdown()\n" +
		"\n" +
		"\t// DagNats owns the embedded NATS on :4222 and must boot first.\n" +
		"\tstartDagNats(cfg, pb, todoH)\n" +
		"\tdefer shutdownDagNats()\n" +
		"\n" +
		"\tjs := startNATS(cfg)\n"
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
	if !strings.Contains(string(out), "_ = todoH") {
		t.Errorf("blank todoH use missing after trim:\n%s", out)
	}
	if !strings.Contains(string(out), "js := startNATS(cfg)") {
		t.Errorf("code after the strip was damaged:\n%s", out)
	}
}

func TestStripWhiteboardRemovesCallLineAndDesktopPhaseC(t *testing.T) {
	// Phase 2 Init shape: trimming the whiteboard is a single call-line
	// drop in router.go. The desktop Phase C demo (brace-mode block) must
	// go too, or `go mod tidy` (which parses cmd/desktop) fails.
	dir := t.TempDir()
	routerDir := filepath.Join(dir, "router")
	desktopDir := filepath.Join(dir, "cmd", "desktop")
	if err := os.MkdirAll(routerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(desktopDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\t\t// Optional capabilities: one call each.\n" +
		"\t\tregisterOnboarding(app, q, se, broadcaster, todoH, cfg)\n" +
		"\t\tregisterWhiteboardStack(se, q, cfg)\n" +
		"\n" +
		"\t\tif cfg.OfflineSync.Enabled {\n"
	if err := os.WriteFile(filepath.Join(routerDir, "router.go"), []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	dgive := "import (\n\t\"context\"\n\t\"time\"\n\n" +
		"\t\"github.com/calionauta/gogogo/internal/collab\"\n)\n" +
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
	out, _ := os.ReadFile(filepath.Join(routerDir, "router.go"))
	if strings.Contains(string(out), "WhiteboardStack") || strings.Contains(string(out), "internal/collab") {
		t.Errorf("whiteboard wiring survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), "registerOnboarding") {
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
