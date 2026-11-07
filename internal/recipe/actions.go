// The action table: the invocable surface, as data.
//
// Discovery answers "what exists"; an action answers "what may run". The
// table is deliberately small — a closed verb bound to a declared route —
// because the property that matters is two-way: an action whose route is
// not declared is uncallable, and a mutating route no action names is
// unreachable headless. Dispatch and the auth bridge are separate phases;
// this file only validates the table.
package recipe

import "fmt"

// Action is one headless-callable verb. Verb is the closed name an agent
// calls (create, complete, …); Route must match a declared route exactly
// (method + absolute pattern), the same convention the composition root
// flattens with MountFeature.
type InvokeAction struct {
	Verb  string `yaml:"verb"`
	Route Route  `yaml:"route"`
}

// actionProblems validates the action table against the routes half of the
// same declaration. It is pure data in, strings out: no filesystem, no
// handler — so the gate can run in a unit test and at boot alike.
func actionProblems(d Declaration) []string { return ActionProblems(d) }

// ActionProblems is the exported gate for composed-tree checks: every
// verb must target a route the same declaration claims. The receipt drift
// check and the server's declaration-vs-mounts gate share this one
// definition so a drifted table is refused in both places.
func ActionProblems(d Declaration) []string {
	declared := map[Route]bool{}
	for _, r := range d.Routes {
		declared[r] = true
	}
	var problems []string
	seen := map[string]bool{}
	for _, a := range d.Actions {
		if a.Verb == "" {
			problems = append(problems, "action with empty verb")
			continue
		}
		if seen[a.Verb] {
			problems = append(problems, fmt.Sprintf("duplicate action verb %q", a.Verb))
		}
		seen[a.Verb] = true
		if !declared[a.Route] {
			problems = append(problems,
				fmt.Sprintf("action %q targets undeclared route %s %s",
					a.Verb, a.Route.Method, a.Route.Pattern))
		}
	}
	return problems
}
