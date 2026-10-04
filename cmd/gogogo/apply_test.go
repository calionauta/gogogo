package main

// Apply, run and check tests: the end-to-end CLI behaviour.

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/capabilities"
)

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
	// Skipped wholesale in evolved trees: once any owned path is gone
	// the tree was trimmed, and marker absence there means "removed by
	// design" or "manual follow-up pending" (e.g. sounds call sites) —
	// not drift. Use `gogogo --check` for a human-readable verdict there.
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	for _, c := range capabilities.All {
		for _, p := range append(append([]string{}, c.Dirs...), c.Files...) {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
				t.Skipf("evolved tree (missing %s): drift gate applies to pristine checkouts", p)
			}
		}
	}
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

// trimmedAway reports whether every owned path of the given capabilities
// is absent from root: the unit was fully trimmed and its check problems
// are expected, not drift.
func trimmedAway(root string, capIDs []string) bool {
	byID := capabilities.ByID()
	anyOwned := false
	for _, id := range capIDs {
		c, ok := byID[id]
		if !ok {
			return false
		}
		for _, d := range c.Dirs {
			anyOwned = true
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(d))); err == nil {
				return false
			}
		}
		for _, f := range c.Files {
			anyOwned = true
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f))); err == nil {
				return false
			}
		}
	}
	return anyOwned
}

func TestCheckTreeSkipsTrimmedUnits(t *testing.T) {
	// In a scaffolded project (trim already applied), fully-trimmed
	// units must be skipped, not failed: their paths are gone by design.
	// Only markers missing from PRESENT files count as drift.
	dir := t.TempDir()
	if !trimmedAway(dir, []string{"landing"}) {
		t.Errorf("empty tree: landing should count as trimmed away")
	}
	present := t.TempDir()
	if err := os.MkdirAll(filepath.Join(present, "features", "landing"), 0o755); err != nil {
		t.Fatal(err)
	}
	if trimmedAway(present, []string{"landing"}) {
		t.Errorf("present landing dir should not count as trimmed away")
	}
	if trimmedAway(present, []string{"no-such-cap"}) {
		t.Errorf("unknown capability must not count as trimmed away")
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
