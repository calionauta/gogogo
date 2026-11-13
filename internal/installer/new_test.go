// Installer engine: `gogogo new` scaffold tests.
package installer

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/calionauta/gogogo/internal/capabilities"
	"github.com/calionauta/gogogo/internal/prompts"
	"github.com/calionauta/gogogo/internal/recipe"
)

// TestValidateNewName pins the id contract: kebab-case, unique, and never a
// route prefix the app already owns.
func TestValidateNewName(t *testing.T) {
	for _, name := range []string{"reports", "a1", "sales-pipeline"} {
		if err := validateNewName(name); err != nil {
			t.Errorf("validateNewName(%q) = %v, want nil", name, err)
		}
	}
	for _, name := range []string{"", "a", "Reports", "report_s", "-reports", "reports-", "todo", "api", "health"} {
		if err := validateNewName(name); err == nil {
			t.Errorf("validateNewName(%q) = nil, want an error", name)
		}
	}
}

// chiFixture mirrors the two anchors the scaffold edits: the todo import
// and the console mount line in BootChi. Neither belongs to a built-in unit, which
// is what makes the scaffold work on trimmed trees.
const chiFixture = `package server

import (
	"net/http"

	todohandlers "github.com/acme/app/features/todo/handlers"
)

func boot() {
	mux := buildChiMux(a, todoH)
	mountConsoleProxy(mux, a, cfg)
	_ = mux
}

func buildChiMux(a string, todoH string) *Router {
	r := NewRouter()
	_ = todohandlers.NewHandler
	_ = todoH
	return r
}
`

// TestEditChiGoAddsWiring proves the self-mounting scaffold needs no chi.go
// edit (wiring rides the registry + units.gen.go) and the result parses.
// Red-proof: it must NOT add the old manual MountFeature (that would
// double-mount and panic).
func TestEditChiGoAddsWiring(t *testing.T) {
	out, err := editChiGo(chiFixture, "reports", "github.com/acme/app")
	if err != nil {
		t.Fatalf("editChiGo: %v", err)
	}
	if strings.Contains(out, "reportshandlers") || strings.Contains(out, `mux.MountFeature("reports"`) {
		t.Errorf("self-mounting scaffold must not edit chi.go (double-mount):\n%s", out)
	}
	if _, perr := parser.ParseFile(token.NewFileSet(), "chi.go", out, parser.AllErrors); perr != nil {
		t.Fatalf("edited chi.go does not parse: %v\n%s", perr, out)
	}
}

// TestNewScaffoldAnchorsPresentInTemplate is the drift gate: the template's
// chi.go must still parse (self-mounting scaffolds edit no anchor there).
// Wiring rides the registry, so moving a mount line cannot silently refuse
// every `gogogo new` — the registry anchor is the drift gate instead.
func TestNewScaffoldAnchorsPresentInTemplate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "server", "chi.go"))
	if err != nil {
		t.Fatalf("read chi.go: %v", err)
	}
	out, err := editChiGo(string(raw), "reports", "github.com/calionauta/gogogo")
	if err != nil {
		t.Fatalf("editChiGo against the template: %v", err)
	}
	if strings.Contains(out, `features/reports/handlers`) {
		t.Errorf("self-mounting scaffold must not edit chi.go:\n%s", out)
	}
	if _, perr := parser.ParseFile(token.NewFileSet(), "chi.go", out, parser.AllErrors); perr != nil {
		t.Fatalf("scaffolded chi.go does not parse: %v", perr)
	}
}

// TestEditChiGoRefusesMissingAnchor: an invalid chi.go (e.g. a truncated
// file) is refused — self-mounting still validates the composition root
// parses, even though it edits nothing.
func TestEditChiGoRefusesMissingAnchor(t *testing.T) {
	if _, err := editChiGo("package server\nfunc @bad( {\n", "reports", "github.com/acme/app"); err == nil {
		t.Fatal("editChiGo on unparsesable chi.go = nil, want refusal")
	}
}

// TestEditRegistryAppendsUnit proves the entry lands inside All and parses.
func TestEditRegistryAppendsUnit(t *testing.T) {
	fixture := "package capabilities\n\nvar All = []Capability{\n\t{ID: \"x\"},\n}\n"
	out, err := editRegistry(fixture, "reports")
	if err != nil {
		t.Fatalf("editRegistry: %v", err)
	}
	if !strings.Contains(out, `ID:      "reports"`) || !strings.Contains(out, `Dirs:    []string{"features/reports"}`) {
		t.Errorf("registry entry missing:\n%s", out)
	}
	if _, perr := parser.ParseFile(token.NewFileSet(), "registry.go", out, parser.AllErrors); perr != nil {
		t.Fatalf("edited registry does not parse: %v\n%s", perr, out)
	}
	if strings.Count(out, "KindFeature") != 1 {
		t.Errorf("expected exactly one scaffolded unit:\n%s", out)
	}
}

// TestEditRegistryRefusesUnexpectedShape keeps the appender honest: it only
// accepts a file that ends with `var All`.
func TestEditRegistryRefusesUnexpectedShape(t *testing.T) {
	if _, err := editRegistry("package capabilities\n\nvar All = []Capability{}\nvar Other = 1\n", "reports"); err == nil {
		t.Fatal("editRegistry without the closing shape = nil, want refusal")
	}
}

// TestEditRegistryAppendsToAppendWrappedRegistry pins the current shape:
// var All = append([]Capability{…}, append(…)) — the entry must land
// inside the literal, before "}, append(", not after the file.
func TestEditRegistryAppendsToAppendWrappedRegistry(t *testing.T) {
	fixture := "package capabilities\n\nvar extra []Capability\n\n" +
		"var All = append([]Capability{\n\t{ID: \"x\"},\n}, extra...)\n"
	out, err := editRegistry(fixture, "reports")
	if err != nil {
		t.Fatalf("editRegistry (append shape): %v", err)
	}
	if !strings.Contains(out, `ID:      "reports"`) {
		t.Errorf("append-wrapped entry missing:\n%s", out)
	}
	if _, perr := parser.ParseFile(token.NewFileSet(), "registry.go", out, parser.AllErrors); perr != nil {
		t.Fatalf("edited append-wrapped registry does not parse: %v\n%s", perr, out)
	}
	if strings.Count(out, `ID:      "reports"`) != 1 {
		t.Errorf("expected exactly one scaffolded unit:\n%s", out)
	}
}

// TestAppendPolicyRows is the RBAC half: two literal user rows, appended
// once, and stable on a re-run.
func TestAppendPolicyRows(t *testing.T) {
	base := "p, admin, /*, .*\n"
	out := appendPolicyRows(base, "reports")
	for _, want := range []string{"p, user, /reports, .*", "p, user, /api/reports/ping, .*"} {
		if !strings.Contains(out, want) {
			t.Errorf("policy rows missing %q:\n%s", want, out)
		}
	}
	if again := appendPolicyRows(out, "reports"); again != out {
		t.Error("appendPolicyRows is not idempotent")
	}
	// A file without a trailing newline must not glue the rows onto the last rule.
	noNewline := appendPolicyRows("p, admin, /*, .*", "reports")
	if !strings.Contains(noNewline, ".*\n#") {
		t.Errorf("rows glued to the previous rule:\n%s", noNewline)
	}
}

// TestNewSourcesParseAndUseTheShell covers the templates: the handler must
// parse, and the page must render through internal/ui.Shell without importing a
// feature package. The shell owns the sidebar, assets and sound toggle, so a
// scaffolded page has no authviews/sounds variant to trim.
func TestNewSourcesParseAndUseTheShell(t *testing.T) {
	handler := newHandlerSource("reports", "github.com/acme/app")
	if _, err := parser.ParseFile(token.NewFileSet(), "chi.go", handler, parser.AllErrors); err != nil {
		t.Fatalf("handler template does not parse: %v\n%s", err, handler)
	}
	page := newPageSource("reports", "github.com/acme/app")
	if !strings.Contains(page, `"github.com/acme/app/internal/ui"`) || !strings.Contains(page, "@ui.Shell(") {
		t.Errorf("page does not render through internal/ui.Shell:\n%s", page)
	}
	for _, forbidden := range []string{"features/authviews", "features/sounds", "@sounds."} {
		if strings.Contains(page, forbidden) {
			t.Errorf("page imports %q; the shell owns it:\n%s", forbidden, page)
		}
	}
}

// TestNewRefusesToolchainMismatch pins the gate on the `new feature` path:
// a toolchain the tree cannot use is refused BEFORE the generated files land,
// so a failed scaffold never leaves a half-written feature behind.
//
// Red-proof: drop the checkToolchainForTree call from applyNew and this test
// fails — the handler file is written and the run returns nil.
func TestNewRefusesToolchainMismatch(t *testing.T) {
	proj := t.TempDir()
	writeGoMod(t, proj, "1.27.3")
	stubToolchain(t, "go1.27.2", "go1.27.2")

	opt := newOptions{name: "reports", dir: proj}
	plan := newPlan{paths: newProjectPaths(opt.name), module: "example.com/proj"}
	err := applyNew(context.Background(), opt, plan, devNull(t))
	if err == nil || !strings.Contains(err.Error(), "needs go1.27.3") {
		t.Fatalf("new feature must refuse this toolchain, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(proj, "features", "reports")); statErr == nil {
		t.Error("the refused scaffold wrote the feature anyway")
	}
}

// TestNewScaffoldStartsHealthyWithoutMetrics pins the fresh-clone gap: a
// scaffolded feature owns no table, so it must not declare an aspirational
// metric (which would read a table nobody created) and its receipt must
// pin the pristine hashes just written — otherwise `recipe check` is blocked
// (declaration cannot be checked) while the scaffold's own build proof is
// green, and `doctor` fails on a fresh checkout.
func TestNewScaffoldStartsHealthyWithoutMetrics(t *testing.T) {
	recipeSrc := newRecipeSource("reports", "github.com/acme/app")
	if strings.Contains(recipeSrc, "Metrics: []recipe.Metric{") {
		t.Errorf("scaffolded declaration must not declare Metrics (no table exists):\n%s", recipeSrc)
	}
	files := map[string]string{
		"features/reports/handlers/chi.go":    newHandlerSource("reports", "github.com/acme/app"),
		"features/reports/handlers/recipe.go": newRecipeSource("reports", "github.com/acme/app"),
	}
	sources := make(map[string]string, len(files))
	for rel, src := range files {
		sources[rel] = hashForTest(src)
	}
	receipt := newReceiptSource("reports", sources)
	if strings.Contains(receipt, "metrics:") || strings.Contains(receipt, "ext_reports") {
		t.Errorf("scaffolded receipt must not declare metrics:\n%s", receipt)
	}
	if strings.Contains(receipt, "files: {}") || !strings.Contains(receipt, "features/reports/handlers/chi.go: ") {
		t.Errorf("scaffolded receipt must pin the pristine file hashes:\n%s", receipt)
	}
	// A scaffold is the first recipe an author sees: it must carry the
	// discovery half (summary, one described action) or the author has to
	// infer the headless surface from prose.
	for _, want := range []string{"summary:", "actions:", "verb: ping", "description:"} {
		if !strings.Contains(receipt, want) {
			t.Errorf("scaffolded receipt must declare %q for headless discovery:\n%s", want, receipt)
		}
	}
	for _, want := range []string{"Actions: []recipe.InvokeAction", "Description:", `"ping"`} {
		if !strings.Contains(recipeSrc, want) {
			t.Errorf("scaffolded declaration must carry %q:\n%s", want, recipeSrc)
		}
	}
	if registry := newRegistryEntry("reports"); !strings.Contains(registry, `Actions: []string{"ping"}`) {
		t.Errorf("scaffolded capability must mirror the ping verb:\n%s", registry)
	}
	var parsed recipe.Receipt
	if err := yaml.Unmarshal([]byte(receipt), &parsed); err != nil {
		t.Fatalf("scaffolded receipt is not valid YAML: %v\n%s", err, receipt)
	}
	if parsed.Summary == "" || len(parsed.Declaration.Actions) != 1 || parsed.Declaration.Actions[0].Verb != "ping" {
		t.Fatalf("scaffolded receipt lost summary/action: %+v", parsed)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "recipe.go", recipeSrc, parser.AllErrors); err != nil {
		t.Fatalf("scaffolded declaration does not parse: %v\n%s", err, recipeSrc)
	}
}

// TestNewScaffoldWritesABoundary pins the card the scaffold writes: it must
// satisfy the gate that reads it (owns / does-not-own / language / invariants),
// because a scaffold that leaves a heading behind produces a tree whose own
// gate fails on the next commit.
//
// Red-proof: drop the `**Owns.**` line from newCardTemplate and this test
// fails — the card parses with an empty boundary.
func TestNewScaffoldWritesABoundary(t *testing.T) {
	card := capabilities.ParseCard(newCardSource("reports"))
	if card.Owns == "" {
		t.Error("the scaffolded card has no owns line")
	}
	if !strings.Contains(card.Owns, "/reports") || !strings.Contains(card.Owns, "/api/reports/ping") {
		t.Errorf("the scaffolded card must name the routes the scaffold registers: %q", card.Owns)
	}
	if card.DoesNotOwn == "" {
		t.Error("the scaffolded card has no does-not-own line")
	}
	if len(card.Language) == 0 {
		t.Error("the scaffolded card has no language row")
	}
	if len(card.Invariants) == 0 {
		t.Error("the scaffolded card has no invariant")
	}
}

// TestTheCardIsNotReceipted pins the ownership split: the card documents the
// slice, so editing it must not read as drift in the code it describes. A
// receipted card would make `recipe status` report the author's rewrite of the
// boundary as a modified unit file.
func TestTheCardIsNotReceipted(t *testing.T) {
	receipt := newReceiptSource("reports", map[string]string{
		"features/reports/handlers/chi.go": hashForTest(newHandlerSource("reports", "example.com/app")),
	})
	if strings.Contains(receipt, "docs/domain/") || strings.Contains(receipt, capabilities.CardDir) {
		t.Errorf("the receipt must not pin the card (it is documentation, not unit code):\n%s", receipt)
	}
}

// TestNewScaffoldWritesThePromptFile pins the pattern the gate enforces: the
// scaffolded feature's panel prompt is a FILE, the panel and the declaration
// load it by id, and the file carries the same sentence the receipt pins.
// A scaffold that wrote the prompt inline would be caught by check-prompts in
// the tree it just created — so the template must not teach the pattern the
// gate forbids.
//
// Red-proof: put the sentence back into newPanelTemplate and this test
// fails — the panel would load a file that says nothing about the feature.
func TestNewScaffoldWritesThePromptFile(t *testing.T) {
	src := newPanelPromptSource("reports")
	parsed, err := prompts.Parse("panels/reports", []byte(src))
	if err != nil {
		t.Fatalf("the scaffolded prompt file is not a valid prompt: %v\n%s", err, src)
	}
	if parsed.Body != panelPromptText("reports") {
		t.Errorf("the prompt file body %q must be the sentence the receipt pins", parsed.Body)
	}
	panel := newPanelSource("reports", "github.com/acme/app")
	if !strings.Contains(panel, `prompts.Text("panels/reports")`) {
		t.Errorf("the scaffolded panel must load its prompt by id:\n%s", panel)
	}
	decl := newRecipeSource("reports", "github.com/acme/app")
	if !strings.Contains(decl, `prompts.Text("panels/reports")`) {
		t.Errorf("the scaffolded declaration must load its prompt by id:\n%s", decl)
	}
	for name, code := range map[string]string{"recipe.go": decl, "genui_panel.go": panel} {
		if _, perr := parser.ParseFile(token.NewFileSet(), name, code, parser.AllErrors); perr != nil {
			t.Errorf("the scaffolded %s does not parse: %v\n%s", name, perr, code)
		}
	}
}

func hashForTest(src string) string {
	return recipe.Hash([]byte(src))
}
