// SCOPE:plugin — test install of the panel registry.
//
// The registry is installed ONCE at boot (a second install is an error, by
// design: changing the model's vocabulary at runtime is not a thing to allow).
// Tests therefore install their own stub provider through TestMain, and the
// production install path stays single-shot.
//
// Why TestMain rather than each test: the registry is process state, and
// re-installing per test would need a reset escape hatch — exactly the
// runtime-mutation seam the single-shot rule exists to close. One install,
// read-only afterwards, and the tests that care about the ABSENCE of a panel
// pass their own `providers` slice to Hydrate (which is a parameter, not
// registry state).
package components

import (
	"context"
	"fmt"
	"html/template"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// TestMain installs the stub panel so IsPanel("todo_list") is true for every
// test in this package, which is what production looks like after boot.
//
// GOGOGO_TEST_TWO_PANELS=1 installs a SECOND panel as well, before any test
// runs. That is how registry_dynamic_test.go proves the extensibility claim:
// the registry freezes the vocabulary at boot (a later change is refused, by
// design), so "adding a panel needs only a provider" can only be demonstrated
// by booting with the extra provider in place. A subprocess is the honest way
// to do that — relaxing the guard for the test would delete the thing under
// test.
// TestMain installs the package's stub panels so the registry is populated for
// every test in this package — which is what production looks like after boot.
//
// TWO panels are installed, and the second is not decoration: one works, one
// ALWAYS FAILS. The failing one exists so the drop-on-failure path
// (hydrate_test.go) has a panel to fail on WITHOUT swapping the registry
// mid-run. That swap was a real cross-test bug: the install re-binds providers
// by NAME, so a same-name swap changed behaviour for every parallel test in the
// package, and a later test failed for a reason three files away.
//
// GOGOGO_TEST_TWO_PANELS=1 adds a THIRD panel, before any test runs. That is how
// registry_dynamic_test.go proves the extensibility claim: the registry freezes
// the vocabulary at boot (a later change is refused, by design), so "adding a
// panel needs only a provider" can only be demonstrated by booting with the
// extra provider in place. A subprocess is the honest way to do that — relaxing
// the guard for the test would delete the thing under test.
func TestMain(m *testing.M) {
	providers := []PanelProvider{stubTestPanel{}, failingStubPanel{}, digestStubPanel{}}
	if os.Getenv("GOGOGO_TEST_TWO_PANELS") == "1" {
		providers = append(providers, secondStubPanel{})
	}
	if err := InstallPanelRegistry(providers); err != nil {
		panic("components: test registry install failed: " + err.Error())
	}
	// Actions too: an undeclared action is refused (checkDeclaredAction), so a
	// fixture that exercises action rendering has to declare it exactly as a
	// feature does. Installed in TestMain because the registry is process scope.
	if aerr := InstallActionRegistry([]ActionProvider{stubTestActions{}}); aerr != nil {
		panic("components: test action registry install failed: " + aerr.Error())
	}
	os.Exit(m.Run())
}

// panelNameInTest and friends are the STUB panel's shape: the catalog seam is
// provider-based and knows no feature, so these tests define their own minimal
// panel rather than importing the todo feature (which would invert the
// dependency the seam exists to keep one-way).
const panelNameInTest = "todo_list"

var todoFiltersInTest = []string{"open", "done", "all"}

// TodoRowInTest is the stub's hydrated row shape.
type TodoRowInTest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// stubPanelView is the stub panel's renderer, and it is deliberately NOT a
// Templ file: a `*_test.templ` generates `*_test_templ.go`, which does not end
// in `_test.go` and is therefore compiled into the PRODUCTION package — where
// a test-only row type does not exist (measured: `undefined: TodoRowInTest`).
// A raw fragment keeps the stub in the test build, which is where it belongs.
func stubPanelView(filter string, rows []TodoRowInTest) templ.Component {
	var b strings.Builder
	b.WriteString(`<div id="stub-panel" data-filter="` + template.HTMLEscapeString(filter) + `">`)
	for _, r := range rows {
		b.WriteString("<span>" + template.HTMLEscapeString(r.Title) + "</span>")
	}
	b.WriteString("</div>")
	return templ.Raw(b.String())
}

// stubTestPanel is the in-package provider: a panel with a known name and one
// writable prop, so the prompt/parse/hydrate/render paths have something real
// to exercise without a store.
type stubTestPanel struct{}

func (stubTestPanel) Component() string { return panelNameInTest }

// stubTestPanel declares its aliases, which is now the ONLY way an alias
// exists: the shared catalog holds none of its own (see PanelAliaiser). The
// spellings mirror what the real todo provider announces, so the alias tests
// exercise the same names a model actually writes.
func (stubTestPanel) Aliases() []string { return []string{"todos", "todo", "task_list", "tasks"} }
func (stubTestPanel) Prompt() string    { return "todo_list {filter: open|done|all} (test stub)" }
func (stubTestPanel) Writable() []string {
	return []string{"filter"}
}

func (stubTestPanel) Fill(_ context.Context, _ string, props map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(props)+1)
	maps.Copy(out, props)
	out["rows"] = []TodoRowInTest{}
	return out, nil
}

func (stubTestPanel) Render(props map[string]any) (templ.Component, error) {
	filter, _ := props["filter"].(string)
	// Validate like production does: a stub that accepts anything would make
	// the enum's fail-closed contract untestable through the registry path.
	if !slices.Contains(todoFiltersInTest, filter) {
		return nil, fmt.Errorf("genui: todo_list filter %q is not one of %v", filter, todoFiltersInTest)
	}
	rows, _ := props["rows"].([]TodoRowInTest)
	return stubPanelView(filter, rows), nil
}

// stubTestActions declares the actions the catalog fixtures emit, so the
// declared-action gate has something to accept. The URLs mirror what the
// fixtures already used before the gate existed.
type stubTestActions struct{}

func (stubTestActions) Actions() []ActionSpec {
	return []ActionSpec{
		{
			Label: "Apply", Method: "POST", URL: "/api/todos",
			Fields: map[string]string{"title": "the todo's text"},
			When:   "the user wants a task added to their list",
		},
		{
			Label: "Add to Calendar", Method: "POST", URL: "/addToCalendar",
			Fields: map[string]string{}, When: "the user wants a plan added to a calendar",
		},
		{
			Label: "Set Reminder", Method: "POST", URL: "/setReminder",
			Fields: map[string]string{}, When: "the user wants a reminder set",
		},
		{
			Label: "Add Activity", Method: "POST", URL: "/add-activity",
			Fields: map[string]string{}, When: "the user wants an activity added",
		},
	}
}

// digestStubPanel is the ONLY provider in the test registry that implements the
// optional PanelSummarizer seam, and it is installed here because the registry
// is install-once by construction (a runtime vocabulary change is exactly what
// InstallPanelRegistry refuses). The wiring tests need a digest to exist;
// tests that assert "no summarizer" behaviour use DataContext's own guard
// instead of reinstalling.
//
// The digest is fixed, not derived from the store: the point of these tests is
// the CONCATENATION into the prompt, not the query (the todo feature proves the
// query, against a real database).
type digestStubPanel struct{ stubTestPanel }

const digestStubSummary = "3 todo(s) total (2 open, 1 done). Open: ship parser; write note."

func (digestStubPanel) Component() string { return "digest_list" }

// digestStubPanel must NOT inherit the stub's aliases: it embeds stubTestPanel
// for the render methods, and the install correctly refuses an alias claimed by
// two panels (which is what the collision guard caught here). Declaring its own
// keeps the two panels unambiguous.
func (digestStubPanel) Aliases() []string { return []string{"digest"} }

func (digestStubPanel) Prompt() string {
	return "digest_list {filter: open|done|all} (test stub with a prompt digest)"
}

func (digestStubPanel) Summary(context.Context, string) (string, error) {
	return digestStubSummary, nil
}
