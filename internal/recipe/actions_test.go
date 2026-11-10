package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestActionTableWiresThroughCheck proves checkDeclaration actually calls
// the action gate: a receipt whose action targets an undeclared route must
// surface the problem through Check, not just through actionProblems.
func TestActionTableWiresThroughCheck(t *testing.T) {
	root := t.TempDir()
	rel := "features/probe/page.go"
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package probe\n\nfunc Routes() { handle(\"GET\", \"/api/probe\", page) }\n"
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Receipt{ID: "probe", Files: map[string]string{rel: Hash([]byte(body))}}
	r.Declaration.Routes = []Route{{Method: "GET", Pattern: "/api/probe"}}
	r.Declaration.Actions = []InvokeAction{
		{Verb: "create", Route: Route{Method: "POST", Pattern: "/api/ghost"}},
	}
	d, err := Check(root, r)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	found := false
	for _, p := range d.Problems {
		if strings.Contains(p, "create") {
			found = true
		}
	}
	if !found {
		t.Fatalf("action problem must surface through Check, got %v", d.Problems)
	}
}

// TestActionTableRefusesUndeclaredRoute is the direction that keeps
// invocation honest: a verb bound to a route the declaration never claimed
// must be refused before any dispatch exists.
func TestActionTableRefusesUndeclaredRoute(t *testing.T) {
	d := Declaration{
		Routes:  []Route{{Method: "POST", Pattern: "/api/probe"}},
		Actions: []InvokeAction{{Verb: "create", Route: Route{Method: "POST", Pattern: "/api/ghost"}}},
	}
	problems := actionProblems(d)
	if len(problems) != 1 {
		t.Fatalf("undeclared target must produce exactly one problem, got %v", problems)
	}
}

// TestActionTableAcceptsMatchingSet closes the vacuous half: a verb whose
// route is declared must stay green, so "no problems" cannot mean "nothing
// checked".
func TestActionTableAcceptsMatchingSet(t *testing.T) {
	d := Declaration{
		Routes:  []Route{{Method: "POST", Pattern: "/api/probe"}},
		Actions: []InvokeAction{{Verb: "create", Route: Route{Method: "POST", Pattern: "/api/probe"}}},
	}
	if problems := actionProblems(d); len(problems) != 0 {
		t.Fatalf("matching set must pass, got %v", problems)
	}
}

// TestActionTableRefusesDuplicateAndEmptyVerb pins the two refusal shapes a
// dispatcher would otherwise silently accept: the same verb twice, and a
// verbless entry.
func TestActionTableRefusesDuplicateAndEmptyVerb(t *testing.T) {
	route := Route{Method: "POST", Pattern: "/api/probe"}
	d := Declaration{
		Routes:  []Route{route},
		Actions: []InvokeAction{{Verb: "create", Route: route}, {Verb: "create", Route: route}, {Verb: "", Route: route}},
	}
	problems := actionProblems(d)
	if len(problems) != 2 {
		t.Fatalf("duplicate + empty must produce two problems, got %v", problems)
	}
}

// TestActionTableRefusesUndocumentedParamHelp pins the closed-set rule for
// the agent-facing help: documenting a key the verb does not accept, or
// leaving a help value empty, makes discovery lie about the shape.
func TestActionTableRefusesUndocumentedParamHelp(t *testing.T) {
	route := Route{Method: "POST", Pattern: "/api/probe"}
	d := Declaration{
		Routes: []Route{route},
		Actions: []InvokeAction{{
			Verb: "create", Route: route, Params: []string{"title"},
			ParamHelp: map[string]string{"title": "", "ghost": "not accepted"},
		}},
	}
	problems := actionProblems(d)
	if len(problems) != 2 {
		t.Fatalf("empty help + undeclared key must produce two problems, got %v", problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"empty help for param \"title\"", "documents param \"ghost\""} {
		if !strings.Contains(joined, want) {
			t.Errorf("problem list misses %q: %v", want, problems)
		}
	}
}

// TestActionPathParamsMustMatchTheRoute pins the closed v1 shape: a
// placeholder and its declaration travel together, the only accepted
// placeholder is `{id}`, and an id is never also a body param. Without this
// gate, discovery can advertise a verb the dispatcher cannot fill.
func TestActionPathParamsMustMatchTheRoute(t *testing.T) {
	good := Declaration{
		Routes: []Route{{Method: "POST", Pattern: "/api/probe/{id}/toggle"}},
		Actions: []InvokeAction{{
			Verb:       "toggle",
			Route:      Route{Method: "POST", Pattern: "/api/probe/{id}/toggle"},
			PathParams: []string{"id"},
			ParamHelp:  map[string]string{"id": "The probe UUID."},
		}},
	}
	if problems := ActionProblems(good); len(problems) != 0 {
		t.Fatalf("declared {id} must pass, got %v", problems)
	}

	for _, tc := range []struct {
		name   string
		action InvokeAction
		want   string
	}{
		{
			name: "placeholder without declaration",
			action: InvokeAction{
				Verb:  "toggle",
				Route: Route{Method: "POST", Pattern: "/api/probe/{id}/toggle"},
			},
			want: "must declare pathParams: [id]",
		},
		{
			name: "declaration without placeholder",
			action: InvokeAction{
				Verb:  "toggle",
				Route: Route{Method: "POST", Pattern: "/api/probe/toggle"}, PathParams: []string{"id"},
			},
			want: "has no placeholder",
		},
		{
			name: "second placeholder is not v1",
			action: InvokeAction{
				Verb:  "move",
				Route: Route{Method: "POST", Pattern: "/api/probe/{id}/move/{target}"}, PathParams: []string{"id"},
			},
			want: "only one {id} placeholder",
		},
		{
			name: "id in body and path",
			action: InvokeAction{
				Verb:   "toggle",
				Route:  Route{Method: "POST", Pattern: "/api/probe/{id}/toggle"},
				Params: []string{"id"}, PathParams: []string{"id"},
			},
			want: "both a body param and a path param",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Declaration{
				Routes:  []Route{{Method: tc.action.Route.Method, Pattern: tc.action.Route.Pattern}},
				Actions: []InvokeAction{tc.action},
			}
			joined := strings.Join(ActionProblems(d), "\n")
			if !strings.Contains(joined, tc.want) {
				t.Fatalf("want %q, got %v", tc.want, joined)
			}
		})
	}
}

// TestOmissionProblemsRequireAReasonAndADeclaredMutatingRoute pins the
// deliberate-omission contract: without it, every intentional headless
// omission is indistinguishable from a forgotten action.
func TestOmissionProblemsRequireAReasonAndADeclaredMutatingRoute(t *testing.T) {
	route := Route{Method: "POST", Pattern: "/api/probe/suggest"}
	good := Declaration{
		Routes: []Route{route},
		Omitted: []OmittedRoute{{
			Route:  route,
			Reason: "asynchronous job; the result arrives through the stream",
		}},
	}
	if problems := OmissionProblems(good); len(problems) != 0 {
		t.Fatalf("a declared omission with a reason must pass, got %v", problems)
	}

	bad := Declaration{
		Routes: []Route{route, {Method: "GET", Pattern: "/api/probe"}, {Method: "POST", Pattern: "/api/probe/{id}/toggle"}},
		Actions: []InvokeAction{{
			Verb: "toggle", Route: Route{Method: "POST", Pattern: "/api/probe/{id}/toggle"},
			PathParams: []string{"id"},
		}},
		Omitted: []OmittedRoute{
			{Route: Route{Method: "POST", Pattern: "/api/ghost"}, Reason: "not declared"},
			{Route: route, Reason: "  "},
			{Route: route, Reason: "duplicate"},
			{Route: Route{Method: "GET", Pattern: "/api/probe"}, Reason: "reads do not need an omission"},
			{Route: Route{Method: "POST", Pattern: "/api/probe/{id}/toggle"}, Reason: "also an action"},
		},
	}
	joined := strings.Join(OmissionProblems(bad), "\n")
	for _, want := range []string{
		"is not declared",
		"has no reason",
		"omitted more than once",
		"only mutating routes",
		"both an action and omitted",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, joined)
		}
	}
}

// TestOmissionProblemsWireThroughCheck proves the omission gate is not a
// detached helper: a receipt with an unexplained omission must surface the
// problem through Check, which is what install/boot consume.
func TestOmissionProblemsWireThroughCheck(t *testing.T) {
	root := t.TempDir()
	rel := "features/probe/page.go"
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package probe\n\nfunc Routes() { handle(\"POST\", \"/api/probe/suggest\", page) }\n"
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	r := Receipt{ID: "probe", Files: map[string]string{rel: Hash([]byte(body))}}
	r.Declaration.Routes = []Route{{Method: "POST", Pattern: "/api/probe/suggest"}}
	r.Declaration.Omitted = []OmittedRoute{{
		Route: Route{Method: "POST", Pattern: "/api/probe/suggest"},
	}}
	d, err := Check(root, r)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(strings.Join(d.Problems, "\n"), "has no reason") {
		t.Fatalf("unexplained omission must surface through Check, got %v", d.Problems)
	}
}

// TestActionDocProblemsRequiresDescription separates the two gates: the
// structural action table keeps booting an older recipe without a
// description, while the review/check half demands the sentence an agent
// needs to choose the verb.
func TestActionDocProblemsRequiresDescription(t *testing.T) {
	route := Route{Method: "POST", Pattern: "/api/probe"}
	d := Declaration{
		Routes:  []Route{route},
		Actions: []InvokeAction{{Verb: "create", Route: route}},
	}
	if problems := actionProblems(d); len(problems) != 0 {
		t.Fatalf("structural gate must stay green, got %v", problems)
	}
	problems := ActionDocProblems(d)
	if len(problems) != 1 || !strings.Contains(problems[0], "no description") {
		t.Fatalf("a verb with no description must be reported, got %v", problems)
	}
	d.Actions[0].Description = "Create one probe row."
	if problems := ActionDocProblems(d); len(problems) != 0 {
		t.Fatalf("a described verb must pass, got %v", problems)
	}
}

// TestMetricAndPanelProblemsPinTheFailFastShapes pins the two validators
// the dashboard and the panel catalog share: a non-COUNT metric, a
// metric with an unbound query, and a panel without a writable slice.
func TestMetricAndPanelProblemsPinTheFailFastShapes(t *testing.T) {
	good := Declaration{Metrics: []Metric{
		{Label: "Items", Query: "SELECT COUNT(*) FROM ext_probe_items WHERE owner = ?"},
		{Label: "Everything", Query: "SELECT COUNT(*) FROM ext_probe_items", OwnerBinds: WithoutOwner()},
	}}
	if problems := MetricProblems(good); len(problems) != 0 {
		t.Fatalf("good metric must pass, got %v", problems)
	}
	bad := Declaration{
		Metrics: []Metric{
			{Label: "Items", Query: "SELECT id FROM ext_probe_items"},
			{Label: "", Query: "SELECT COUNT(*) FROM ext_probe_items WHERE owner = ?"},
		},
		Panel: &PanelSpec{Component: "Bad_Name", Prompt: "when", Writable: nil},
	}
	if problems := MetricProblems(bad); len(problems) != 2 {
		t.Fatalf("two bad metrics must produce two problems, got %v", problems)
	}
	if problems := PanelProblems(bad); len(problems) != 2 {
		t.Fatalf("bad panel name + empty writable must produce two problems, got %v", problems)
	}
	goodPanel := Declaration{Panel: &PanelSpec{
		Component: "probe_list", Prompt: "when the user asks", Writable: []string{"scope"},
	}}
	if problems := PanelProblems(goodPanel); len(problems) != 0 {
		t.Fatalf("good panel must pass, got %v", problems)
	}
}
