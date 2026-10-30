package installer

// Apply, run and check tests: the end-to-end CLI behaviour.

import (
	"context"
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
		err := Run(context.Background(), []string{
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
		err := Run(context.Background(), []string{
			"--name", "my-app", "--no-tui", "--dry-run", "--format", "json",
			"--opt-in", "all",
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
		err := Run(context.Background(), []string{
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
		_ = Run(context.Background(), []string{"--help"}, devNull(t), w)
	})
	for _, want := range []string{
		"Trim units:", "Agent contract:", "Exit codes:",
		"--dry-run", "--yes", "dagnats", "genui",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("--help missing %q", want)
		}
	}
}

func TestCheckKeepIDsFailsFast(t *testing.T) {
	if err := checkKeepIDs("dagnats,sounds", "landing", ""); err != nil {
		t.Errorf("valid ids rejected: %v", err)
	}
	if err := checkKeepIDs("", "", ""); err != nil {
		t.Errorf("empty (keep-all) rejected: %v", err)
	}
	err := checkKeepIDs("dagnats,nats", "todo", "")
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
	// The dagnats unit strips the workflow entry button from the todo
	// page, so it requires regen too — a unit that edits .templ must say
	// so, or the prove step skips generation and ships stale _templ.go.
	if !needsTemplGen([]trimUnit{byID["dagnats"]}) {
		t.Errorf("dagnats drop should require templ regen (it strips .templ buttons)")
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

func TestStripSoundsDropsCallSites(t *testing.T) {
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "features", "chat", "components")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "import (\n" +
		"\t\"github.com/calionauta/gogogo/features/sounds\"\n" +
		")\n" +
		"\n" +
		"templ chatHead() {\n" +
		"\t@sounds.SoundAssets()\n" +
		"}\n" +
		"\n" +
		"templ chatSound() {\n" +
		"\t@sounds.SoundSwitch()\n" +
		"}\n" +
		"\n" +
		"templ keep() {\n" +
		"\t<a href=\"/todo\">Todo</a>\n" +
		"}\n"
	p := filepath.Join(chatDir, "chat.templ")
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
	// The wrappers stay (empty), because the shell's slots take a component.
	if !strings.Contains(string(out), "templ chatHead()") || !strings.Contains(string(out), "templ chatSound()") {
		t.Errorf("the one-line wrappers must survive as empty components:\n%s", out)
	}
	if !strings.Contains(string(out), `href="/todo"`) {
		t.Errorf("unrelated markup was damaged:\n%s", out)
	}
}

// TestStripModuleEntriesForDroppedFeatures pins the sidebar as the navigation
// source: trimming a feature drops its []Module entry, and the pages that stay
// keep theirs. It used to strip <a> blocks out of the navbar template; the
// navbar is gone and the registry is one line per module.
func TestStripModuleEntriesForDroppedFeatures(t *testing.T) {
	dir := t.TempDir()
	uiDir := filepath.Join(dir, "internal", "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "var Modules = []Module{\n" +
		"\t{ID: \"todo\", URL: \"/todo\"},\n" +
		"\t{ID: \"whiteboard\", URL: \"/whiteboard\"},\n" +
		"\t{ID: \"notes\", URL: \"/notes\"},\n" +
		"\t{ID: \"review\", URL: \"/review\"},\n" +
		"\t{ID: \"config\", URL: \"/config\", Admin: true},\n" +
		"}\n"
	p := filepath.Join(uiDir, "modules.go")
	if err := os.WriteFile(p, []byte(give), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := keepAllBut(unitWhiteboard, unitConfigView)
	if err := applyTrim(dir, planTrim(keep), nil); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(p)
	for _, dead := range []string{"whiteboard", "/notes", `{ID: "config"`} {
		if strings.Contains(string(out), dead) {
			t.Errorf("trimmed module %q survived in the registry:\n%s", dead, out)
		}
	}
	for _, live := range []string{`{ID: "todo"`, `{ID: "review"`} {
		if !strings.Contains(string(out), live) {
			t.Errorf("module %q was wrongly removed:\n%s", live, out)
		}
	}
}

func TestLandingRetargetsBrand(t *testing.T) {
	dir := t.TempDir()
	uiDir := filepath.Join(dir, "internal", "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "\t\t\t\t\t\t<a href=\"/\" class=\"app-rail-brand\">\n\t\t\t\t\t\t\tgogogo\n\t\t\t\t\t\t</a>\n"
	p := filepath.Join(uiDir, "shell.templ")
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

func TestCheckTreeReportsMissingMarker(t *testing.T) {
	// Drift detector: a unit whose call line is gone must be reported
	// with the marker named, not silently skipped.
	dir := t.TempDir()
	routerDir := filepath.Join(dir, "internal", "server")
	if err := os.MkdirAll(routerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	give := "import (\n" +
		"\t\"github.com/calionauta/gogogo/features/landing\"\n" +
		")\n" +
		"\treturn r\n"
	if err := os.WriteFile(filepath.Join(routerDir, "chi.go"), []byte(give), 0o600); err != nil {
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
		if strings.Contains(p, `r.NotFound(landingH.Routes().ServeHTTP)`) {
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
	// design" or "manual follow-up pending" (e.g. sounds call sites)
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
