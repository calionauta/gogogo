// SCOPE:plugin — installer engine: `gogogo new` scaffold tests.
package installer

import (
	"go/parser"
	"go/token"
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
// and the stream-status mount after buildChiMux. Neither belongs to a
// trim unit, which is what makes the scaffold work on trimmed trees.
const chiFixture = `package server

import (
	"net/http"

	confighandlers "github.com/acme/app/features/config/handlers"
)

func boot() {
	mux := buildChiMux(a, todoH)
	mux.HandleFunc("/api/stream/status", StreamStatusHandler(hub))
	_ = mux
}

func buildChiMux(a string, todoH string) *http.ServeMux {
	mux := http.NewServeMux()
	_ = confighandlers.NewHandler
	_ = todoH
	return mux
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
		"reportsH := reportshandlers.NewHandler(a)",
		`mux.Handle("/reports", reportsH.Routes())`,
		`mux.Handle("/reports/", reportsH.Routes())`,
		`mux.Handle("/api/reports/", reportsH.Routes())`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("wiring missing %q:\n%s", want, out)
		}
	}
	if _, perr := parser.ParseFile(token.NewFileSet(), "chi.go", out, parser.AllErrors); perr != nil {
		t.Fatalf("edited chi.go does not parse: %v\n%s", perr, out)
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

// TestNewSourcesParseAndDropSounds covers both template variants: the handler
// must parse, and a tree without the sounds unit must not import it.
func TestNewSourcesParseAndDropSounds(t *testing.T) {
	handler := newHandlerSource("reports", "github.com/acme/app")
	if _, err := parser.ParseFile(token.NewFileSet(), "chi.go", handler, parser.AllErrors); err != nil {
		t.Fatalf("handler template does not parse: %v\n%s", err, handler)
	}
	withSounds := newPageSource("reports", "github.com/acme/app", true)
	if !strings.Contains(withSounds, "features/sounds") || !strings.Contains(withSounds, "@sounds.SoundAssets()") {
		t.Errorf("page with sounds is missing its imports:\n%s", withSounds)
	}
	without := newPageSource("reports", "github.com/acme/app", false)
	if strings.Contains(without, "features/sounds") || strings.Contains(without, "@sounds.") {
		t.Errorf("page without sounds still references the package:\n%s", without)
	}
	if !strings.Contains(without, `"github.com/acme/app/features/authviews"`) {
		t.Errorf("page lost the authviews import (the shared navbar):\n%s", without)
	}
}
