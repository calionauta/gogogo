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
