// The action table: the invocable surface, as data.
//
// Discovery answers "what exists"; an action answers "what may run".
// The table is deliberately small — closed verbs bound to declared
// routes with closed param sets — because the property that matters is
// two-way: an action whose route is not declared is uncallable, and a
// route no action names is unreachable headless. This file only
// validates the table; dispatch is separate.
package recipe

import "fmt"

// InvokeAction is one headless-callable verb. Verb is the closed name
// Verb is the name an agent calls (create, complete, …); Route must match
// a declared route exactly (method + absolute pattern), the same
// convention the composition root flattens with MountFeature. Params is
// the closed set of top-level form fields the verb forwards: an
// undeclared key is refused loud, naming the allowed names, instead of
// being silently dropped by the handler. Names only on purpose —
// handlers parse values at the boundary, so typed coercion would be
// artificial precision until an action genuinely takes non-string input.
type InvokeAction struct {
	Verb   string   `yaml:"verb"`
	Route  Route    `yaml:"route"`
	Params []string `yaml:"params,omitempty"`
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
		params := map[string]bool{}
		for _, p := range a.Params {
			if p == "" {
				problems = append(problems, fmt.Sprintf("action %q has an empty param name", a.Verb))
				continue
			}
			if params[p] {
				problems = append(problems, fmt.Sprintf("action %q duplicates param %q", a.Verb, p))
			}
			params[p] = true
		}
		if !invocableMethod(a.Route.Method) {
			problems = append(problems,
				fmt.Sprintf("action %q uses method %q, which cannot be invoked headless", a.Verb, a.Route.Method))
		}
		if streamPath(a.Route.Pattern) {
			problems = append(problems,
				fmt.Sprintf("action %q targets streaming route %q, which would hang an invoke", a.Verb, a.Route.Pattern))
		}
	}
	return problems
}

// invocableMethod is the dispatcher's method rule, shared by the
// declaration gate and the invoke bridge: GET (reads) plus the mutating
// methods, never HEAD.
func invocableMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// streamSuffix is the length of the shortest streaming suffix ("/stream").
const streamSuffix = len("/stream")

// streamPath names the routes that never complete on a recorder:
// Server-Sent Events would hang an invoke forever.
func streamPath(pattern string) bool {
	if len(pattern) < streamSuffix {
		return false
	}
	return pattern[len(pattern)-streamSuffix:] == "/stream" || pattern[len(pattern)-streamSuffix:] == "/events"
}
