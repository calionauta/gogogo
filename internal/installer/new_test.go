// SCOPE:plugin — installer engine: `gogogo new` scaffold tests.
package installer

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// chiFixture mirrors the two anchors the scaffold edits: the config import
// and the console mount line in BootChi. Neither belongs to a trim unit, which
// is what makes the scaffold work on trimmed trees.
const chiFixture = `package server

import (
	"net/http"

	confighandlers "github.com/acme/app/features/config/handlers"
)

func boot() {
	mux := buildChiMux(a, todoH)
	mountConsoleProxy(mux, a, cfg)
	_ = mux
}

func buildChiMux(a string, todoH string) *Router {
	r := NewRouter()
	_ = confighandlers.NewHandler
	_ = todoH
	return r
}
`

// TestEditChiGoAddsWiring proves the four edits land and the result parses.
func TestEditChiGoAddsWiring(t *testing.T) {
	out, err := editChiGo(chiFixture, "reports", "github.com/acme/app")
	if err != nil {
		t.Fatalf("editChiGo: %v", err)
	}
	for _, want := range []string{
		`reportshandlers "github.com/acme/app/features/reports/handlers"`,
		"reportsH := reportshandlers.NewHandler(a, cfg)",
		`mux.MountFeature("reports", reportsH.Routes())`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("wiring missing %q:\n%s", want, out)
		}
	}
	if _, perr := parser.ParseFile(token.NewFileSet(), "chi.go", out, parser.AllErrors); perr != nil {
		t.Fatalf("edited chi.go does not parse: %v\n%s", perr, out)
	}
}

// TestNewScaffoldAnchorsPresentInTemplate is the drift gate behind the
// fixture above: the anchors `gogogo new feature` edits must exist in the
// template's REAL composition root. Moving a mount line without updating the
// scaffold turns the command into a refusal for every user, and a fixture
// cannot notice.
func TestNewScaffoldAnchorsPresentInTemplate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "internal", "server", "chi.go"))
	if err != nil {
		t.Fatalf("read chi.go: %v", err)
	}
	out, err := editChiGo(string(raw), "reports", "github.com/calionauta/gogogo")
	if err != nil {
		t.Fatalf("editChiGo against the template: %v", err)
	}
	for _, want := range []string{
		`reportshandlers "github.com/calionauta/gogogo/features/reports/handlers"`,
		`mux.MountFeature("reports", reportsH.Routes())`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scaffolded chi.go missing %q", want)
		}
	}
}

// TestEditChiGoRefusesMissingAnchor is the safety half: a moved composition
// root must refuse before the caller writes anything.
func TestEditChiGoRefusesMissingAnchor(t *testing.T) {
	if _, err := editChiGo("package server\n", "reports", "github.com/acme/app"); err == nil {
		t.Fatal("editChiGo on a tree without anchors = nil, want refusal")
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
