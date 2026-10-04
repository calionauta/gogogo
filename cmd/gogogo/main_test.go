package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
)

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"my-app", "app2", "a.b_c-d"} {
		if err := validateName(ok); err != nil {
			t.Errorf("validateName(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "-app", "my app", "gogogo"} {
		if err := validateName(bad); err == nil {
			t.Errorf("validateName(%q) = nil, want error", bad)
		}
	}
}

func TestPlanTrimKeepsAllByDefault(t *testing.T) {
	keep := parseKeep("", "")
	if got := planTrim(keep); len(got) != 0 {
		t.Errorf("planTrim(default) dropped %v, want nothing", dropIDs(got))
	}
}

// keepAllBut returns a keep set with every manifest unit kept except dropped.
// It keeps test lines short and stays correct when new units are added.
func keepAllBut(dropped ...string) keepSet {
	skip := map[string]bool{}
	for _, d := range dropped {
		skip[d] = true
	}
	var keep []string
	for _, u := range manifestUnits {
		if !skip[u.id] {
			keep = append(keep, u.id)
		}
	}
	joined := strings.Join(keep, ",")
	return parseKeep(joined, joined)
}

func TestParseKeepDimensionsIndependent(t *testing.T) {
	// Convention: empty means keep-all IN THAT DIMENSION. Answering only
	// the plugins question must not drop every feature.
	keep := parseKeep("dagnats", "")
	drop := planTrim(keep)
	ids := map[string]bool{}
	for _, u := range drop {
		ids[u.id] = true
	}
	if ids["whiteboard"] || ids["landing"] || ids["config-view"] {
		t.Errorf("empty features answer dropped features: %v", dropIDs(drop))
	}
	if !ids["credits"] || !ids["sounds"] || !ids["skins-extra"] {
		t.Errorf("unlisted plugins must drop: %v", dropIDs(drop))
	}
}

func TestParseSelection(t *testing.T) {
	options := []string{"dagnats", "credits", "sounds"}
	cases := []struct {
		input   string
		wantIDs []string
		wantAll bool
		wantErr bool
	}{
		{"", nil, true, false},
		{"none", []string{}, false, false},
		{"NONE", []string{}, false, false},
		{"1", []string{"dagnats"}, false, false},
		{"1,3", []string{"dagnats", "sounds"}, false, false},
		{"3,1,3", []string{"sounds", "dagnats"}, false, false},
		{"credits", []string{"credits"}, false, false},
		{"1,credits", []string{"dagnats", "credits"}, false, false},
		{" 2 , sounds ", []string{"credits", "sounds"}, false, false},
		{"0", nil, false, true},
		{"4", nil, false, true},
		{"nats", nil, false, true},
		{"1,nats", nil, false, true},
		{"abc", nil, false, true},
	}
	for _, tc := range cases {
		ids, all, err := parseSelection(tc.input, options)
		if tc.wantErr != (err != nil) {
			t.Errorf("parseSelection(%q) err = %v, wantErr %v", tc.input, err, tc.wantErr)
			continue
		}
		if all != tc.wantAll {
			t.Errorf("parseSelection(%q) all = %v, want %v", tc.input, all, tc.wantAll)
		}
		if fmt.Sprint(ids) != fmt.Sprint(tc.wantIDs) {
			t.Errorf("parseSelection(%q) = %v, want %v", tc.input, ids, tc.wantIDs)
		}
	}
}

func TestUnitMenusFollowRegistryKinds(t *testing.T) {
	// Menu numbers are positional: pin the order so humans learn stable
	// numbers and prompt options never drift from registry kinds.
	plugins := unitIDsOfKind(capabilities.KindPlugin)
	wantPlugins := []string{"dagnats", "credits", "sounds", "skins-extra"}
	if fmt.Sprint(plugins) != fmt.Sprint(wantPlugins) {
		t.Errorf("plugin menu = %v, want %v", plugins, wantPlugins)
	}
	features := unitIDsOfKind(capabilities.KindFeature)
	wantFeatures := []string{"whiteboard", "landing", "config-view"}
	if fmt.Sprint(features) != fmt.Sprint(wantFeatures) {
		t.Errorf("feature menu = %v, want %v", features, wantFeatures)
	}
}

func TestPromptFormRepairsInvalidAnswer(t *testing.T) {
	// Humans mistype: an invalid plugins answer re-asks instead of
	// aborting or silently mis-trimming.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(w, "my-app")
	fmt.Fprintln(w, "myorg")
	fmt.Fprintln(w, "nats") // invalid: not an installer unit
	fmt.Fprintln(w, "1")    // repair: first plugin (dagnats)
	fmt.Fprintln(w, "")     // features: empty = keep all
	_ = w.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(pr)
		done <- string(raw)
	}()
	res, err := promptForm(r, pw)
	_ = pw.Close()
	transcript := <-done
	if err != nil {
		t.Fatalf("promptForm: %v", err)
	}
	if res.plugins != "dagnats" {
		t.Errorf("plugins = %q, want dagnats", res.plugins)
	}
	if res.features != "" {
		t.Errorf("features = %q, want empty (keep all)", res.features)
	}
	if !strings.Contains(transcript, "unknown") || !strings.Contains(transcript, "try again") {
		t.Errorf("transcript must show the repair loop:\n%s", transcript)
	}
}

func TestParseKeepNoneDropsDimension(t *testing.T) {
	keep := parseKeep("none", "none")
	drop := planTrim(keep)
	if len(drop) != len(manifestUnits) {
		t.Errorf("none/none should drop all %d units, dropped %v", len(manifestUnits), dropIDs(drop))
	}
}

func TestPlanTrimDropsSkipped(t *testing.T) {
	keep := parseKeep("landing", "landing")
	drop := planTrim(keep)
	ids := map[string]bool{}
	for _, u := range drop {
		ids[u.id] = true
	}
	if ids["landing"] {
		t.Errorf("landing was kept but appears in drop set")
	}
	if !ids["dagnats"] || !ids["whiteboard"] || !ids["credits"] {
		t.Errorf("expected dagnats/whiteboard/credits in drop set, got %v", dropIDs(drop))
	}
}

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
	raw, err := os.ReadFile("README.md")
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

func TestRunNoTUIWithoutYesChangesNothing(t *testing.T) {
	// Agents must pin --yes explicitly: --no-tui alone prints the plan
	// and leaves the checkout untouched.
	dir := t.TempDir()
	marker := filepath.Join(dir, "router", "router.go")
	if err := os.MkdirAll(filepath.Join(dir, "router"), 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\t\t// Landing page (public, GET /). Routes the marketing hero\n" +
		"\t\tlanding.New(cfg).RegisterRoutes(se)\n"
	if err := os.WriteFile(marker, []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func(w *os.File) {
		err := run([]string{
			"--name", "my-app", "--no-tui", "--dir", dir,
		}, devNull(t), w)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	raw, _ := os.ReadFile(marker)
	if !strings.Contains(string(raw), "landing.New") {
		t.Errorf("checkout was modified without --yes")
	}
	if !strings.Contains(out, "re-run with --yes") {
		t.Errorf("plan-only hint missing:\n%s", out)
	}
}

func TestPlanJSONEncodesEmptySlices(t *testing.T) {
	// Agent contract stability: empty collections encode as [] never null.
	out := captureStdout(t, func(w *os.File) {
		err := run([]string{
			"--name", "my-app", "--no-tui", "--dry-run", "--format", "json",
			"--dir", "./my-app",
		}, devNull(t), w)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	for _, bad := range []string{`"drop": null`, `"warnings": null`, `"stripsMissed": null`} {
		if strings.Contains(out, bad) {
			t.Errorf("plan JSON contains %s (must be [])", bad)
		}
	}
	var decoded struct {
		Drop []struct {
			Warnings []string `json:"warnings"`
		} `json:"drop"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("plan is not valid JSON: %v", err)
	}
	if len(decoded.Drop) != 0 {
		t.Errorf("keep-all plan should drop nothing, got %v", decoded.Drop)
	}
}

func TestRunDryRunJSONPlan(t *testing.T) { // The agent contract: --dry-run --format json parses and carries
	// drop ids with their warnings.
	out := captureStdout(t, func(w *os.File) {
		err := run([]string{
			"--name", "my-app", "--owner", "myorg",
			"--plugins", "sounds", "--features", "sounds",
			"--no-tui", "--dry-run", "--format", "json",
			"--dir", "./my-app",
		}, devNull(t), w)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	})
	var decoded struct {
		Module string `json:"module"`
		Drop   []struct {
			ID         string   `json:"id"`
			Kind       string   `json:"kind"`
			Warnings   []string `json:"warnings"`
			RuntimeOff string   `json:"runtimeOff"`
		} `json:"drop"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("plan is not valid JSON: %v\n%s", err, out)
	}
	if decoded.Module != "github.com/myorg/my-app" {
		t.Errorf("module = %q", decoded.Module)
	}
	ids := map[string]bool{}
	byID := map[string]string{}
	warned := 0
	for _, u := range decoded.Drop {
		ids[u.ID] = true
		byID[u.ID] = u.RuntimeOff
		warned += len(u.Warnings)
		if u.Kind != "plugin" && u.Kind != "feature" { // planUnit.Kind is the registry string

			t.Errorf("unit %q has kind %q", u.ID, u.Kind)
		}
	}
	if byID["dagnats"] != "DAGNATS_ENABLED=false" {
		t.Errorf("dagnats runtimeOff = %q", byID["dagnats"])
	}
	for _, want := range []string{"dagnats", "whiteboard", "landing", "config-view", "credits"} {
		if !ids[want] {
			t.Errorf("drop set missing %q", want)
		}
	}
	if warned == 0 {
		t.Errorf("no warnings in plan — consequences must be explicit")
	}
}

func TestRunHelpMentionsContract(t *testing.T) {
	out := captureStdout(t, func(w *os.File) {
		_ = run([]string{"--help"}, devNull(t), w)
	})
	for _, want := range []string{
		"Trim units:", "Agent contract:", "Exit codes:",
		"--dry-run", "--yes", "dagnats", "skins-extra",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--help missing %q", want)
		}
	}
}

func TestCheckKeepIDsFailsFast(t *testing.T) {
	if err := checkKeepIDs("dagnats,sounds", "landing"); err != nil {
		t.Errorf("valid ids rejected: %v", err)
	}
	if err := checkKeepIDs("", ""); err != nil {
		t.Errorf("empty (keep-all) rejected: %v", err)
	}
	err := checkKeepIDs("dagnats,nats", "todo")
	if err == nil {
		t.Fatal("unknown ids nats/todo accepted silently")
	}
	for _, want := range []string{"nats", "todo", "dagnats"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestNeedsTemplGen(t *testing.T) {
	byID := map[string]trimUnit{}
	for _, u := range manifestUnits {
		byID[u.id] = u
	}
	gen := []trimUnit{byID["config-view"], byID["sounds"]}
	if !needsTemplGen(gen) {
		t.Errorf("config-view/sounds drop should require templ regen")
	}
	if needsTemplGen([]trimUnit{byID["dagnats"]}) {
		t.Errorf("dagnats-only drop should not require templ regen")
	}
}

func captureStdout(t *testing.T, fn func(w *os.File)) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(r)
		done <- string(raw)
	}()
	fn(w)
	_ = w.Close()
	return <-done
}

func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestStripSkinsExtraDropsBlankImports(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "features", "todo", "components")
	if err := os.MkdirAll(compDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "import (\n" +
		"\t_ \"github.com/calionauta/gogogo/web/skins/basecoat\"\n" +
		"\t_ \"github.com/calionauta/gogogo/web/skins/daisyui\"\n" +
		"\t_ \"github.com/calionauta/gogogo/web/skins/morpheus\"\n" +
		")\n"
	p := filepath.Join(compDir, "skin_imports.go")
	if err := os.WriteFile(p, []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut(unitSkinsExtra)
	if err := applyTrim(dir, planTrim(keep), nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	if strings.Contains(string(out), "basecoat") || strings.Contains(string(out), "morpheus") {
		t.Errorf("skin blank imports survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), "daisyui") {
		t.Errorf("daisyui import was wrongly removed:\n%s", out)
	}
}

func TestStripSoundsDropsCallSites(t *testing.T) {
	dir := t.TempDir()
	authDir := filepath.Join(dir, "features", "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "import \"github.com/calionauta/gogogo/features/sounds\"\n" +
		"\n" +
		"\t\t\t@sounds.SoundToggle()\n" +
		"\t\t\t@sounds.SoundAssets()\n" +
		"\t\t\t<a href=\"/todo\">Todo</a>\n"
	p := filepath.Join(authDir, "views.templ")
	if err := os.WriteFile(p, []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut(unitSounds)
	if err := applyTrim(dir, planTrim(keep), nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	if strings.Contains(string(out), "@sounds.") || strings.Contains(string(out), "features/sounds") {
		t.Errorf("sounds call sites survived trim:\n%s", out)
	}
	if !strings.Contains(string(out), `href="/todo"`) {
		t.Errorf("unrelated navbar link was damaged:\n%s", out)
	}
}

func TestStripNavbarLinksForDroppedFeatures(t *testing.T) {
	dir := t.TempDir()
	authDir := filepath.Join(dir, "features", "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\t\t\t\t<a href=\"/todo\">Todo</a>\n" +
		"\t\t\t\t<a href=\"/whiteboard\" class={ x }>\n" +
		"\t\t\t\t}) }>Whiteboard</a>\n" +
		"\t\t\t\t<a href=\"/config\" class={ y }>\n" +
		"\t\t\t\t}) }>Config</a>\n"
	p := filepath.Join(authDir, "views.templ")
	if err := os.WriteFile(p, []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut(unitWhiteboard, unitConfigView)
	if err := applyTrim(dir, planTrim(keep), nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	for _, dead := range []string{"/whiteboard", "/config", "Whiteboard</a>", "Config</a>"} {
		if strings.Contains(string(out), dead) {
			t.Errorf("dead navbar reference %q survived trim:\n%s", dead, out)
		}
	}
	if !strings.Contains(string(out), `href="/todo"`) {
		t.Errorf("todo navbar link was wrongly removed:\n%s", out)
	}
}

func TestLandingRetargetsBrand(t *testing.T) {
	dir := t.TempDir()
	authDir := filepath.Join(dir, "features", "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\t\t\t<a href=\"/\" class=\"app-nav-brand\">\n\t\t\t\tgogogo\n\t\t\t</a>\n"
	p := filepath.Join(authDir, "views.templ")
	if err := os.WriteFile(p, []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut(unitLanding)
	if err := applyTrim(dir, planTrim(keep), nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	if !strings.Contains(string(out), `href="/todo"`) {
		t.Errorf("brand was not retargeted to /todo:\n%s", out)
	}
	if strings.Contains(string(out), `href="/"`) {
		t.Errorf("dead root href survived trim:\n%s", out)
	}
}

func TestStripSkinsExtraDropsHandlerDispatch(t *testing.T) {
	dir := t.TempDir()
	handlersDir := filepath.Join(dir, "features", "todo", "handlers")
	if err := os.MkdirAll(handlersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	todoGive := "import (\n" +
		"\tmorpheus \"github.com/calionauta/gogogo/web/skins/morpheus\"\n" +
		"\tbasecoat \"github.com/calionauta/gogogo/web/skins/basecoat\"\n" +
		")\n" +
		"\tif skinName == SkinMorpheus {\n" +
		"\t\treturn morpheus.TodoPage(signals).Render(ctx, w)\n" +
		"\t}\n" +
		"\tif skinName == SkinBasecoat {\n" +
		"\t\treturn basecoat.TodoPage(signals).Render(ctx, w)\n" +
		"\t}\n" +
		"\treturn components.Layout(signals).Render(ctx, w)\n"
	repoGive := "\tswitch skinName {\n" +
		"\tcase SkinMorpheus:\n" +
		"\t\treturn morpheus.TodoListRegion(signals)\n" +
		"\tcase SkinBasecoat:\n" +
		"\t\treturn basecoat.TodoListRegion(signals)\n" +
		"\tdefault:\n" +
		"\t\treturn components.TodoListRegion(signals)\n" +
		"\t}\n"
	todoPath := filepath.Join(handlersDir, "todo.go")
	repoPath := filepath.Join(handlersDir, "todo_repo.go")
	if err := os.WriteFile(todoPath, []byte(todoGive), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(repoPath, []byte(repoGive), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut(unitSkinsExtra)
	if err := applyTrim(dir, planTrim(keep), nil); err != nil {
		t.Fatal(err)
	}
	todoOut, _ := os.ReadFile(todoPath)
	for _, dead := range []string{"morpheus", "basecoat", "SkinMorpheus", "SkinBasecoat"} {
		if strings.Contains(string(todoOut), dead) {
			t.Errorf("todo.go still references %q after trim:\n%s", dead, todoOut)
		}
	}
	if !strings.Contains(string(todoOut), "components.Layout") {
		t.Errorf("daisyui default was wrongly removed:\n%s", todoOut)
	}
	repoOut, _ := os.ReadFile(repoPath)
	for _, dead := range []string{"morpheus", "basecoat"} {
		if strings.Contains(string(repoOut), dead) {
			t.Errorf("todo_repo.go still references %q after trim:\n%s", dead, repoOut)
		}
	}
	if !strings.Contains(string(repoOut), "components.TodoListRegion") {
		t.Errorf("daisyui default was wrongly removed:\n%s", repoOut)
	}
}

func TestCheckTreeReportsMissingMarker(t *testing.T) {
	// Drift detector: a unit whose call line is gone must be reported
	// with the marker named, not silently skipped.
	dir := t.TempDir()
	routerDir := filepath.Join(dir, "router")
	if err := os.MkdirAll(routerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "import (\n" +
		"\t\"github.com/calionauta/gogogo/features/landing\"\n" +
		")\n"
	if err := os.WriteFile(filepath.Join(routerDir, "router.go"), []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	results := checkTree(dir)
	var landing *unitCheck
	for i := range results {
		if results[i].ID == unitLanding {
			landing = &results[i]
		}
	}
	if landing == nil {
		t.Fatal("landing unit missing from check results")
	}
	if len(landing.Problems) == 0 {
		t.Errorf("expected problems for call line without marker")
	}
	found := false
	for _, p := range landing.Problems {
		if strings.Contains(p, "landing.New(cfg).RegisterRoutes(se)") {
			found = true
		}
	}
	if !found {
		t.Errorf("problems must name the missing marker: %v", landing.Problems)
	}
}

func TestCheckTreeAgainstRepoRoot(t *testing.T) {
	// The drift gate itself, running in CI: the manifest must match the
	// pristine template checkout. If a source edit moves a marker without
	// updating cmd/gogogo, this fails naming the unit + marker.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	var failed []string
	for _, uc := range checkTree(root) {
		if len(uc.Problems) > 0 {
			failed = append(failed, uc.ID+": "+strings.Join(uc.Problems, "; "))
		}
	}
	if len(failed) > 0 {
		t.Errorf("manifest drifted from source:\n%s", strings.Join(failed, "\n"))
	}
}

func TestWriteAgentsPointsUpstream(t *testing.T) {
	dir := t.TempDir()
	if err := writeAgents(dir, "my-app"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	text := string(raw)
	for _, want := range []string{"my-app", "llms.txt", "scope-taxonomy", "blob/master"} {
		if !strings.Contains(text, want) {
			t.Errorf("AGENTS.md missing %q:\n%s", want, text)
		}
	}
}
