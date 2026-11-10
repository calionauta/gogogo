// The action table: the invocable surface, as data.
//
// Discovery answers "what exists"; an action answers "what may run".
// The table is deliberately small — closed verbs bound to declared
// routes with closed param sets — because the property that matters is
// two-way: an action whose route is not declared is uncallable, and a
// route no action names is unreachable headless. This file only
// validates the table; dispatch is separate.
package recipe

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// InvokeAction is one headless-callable verb. Verb is the name an agent
// calls (create, complete, …); Description says what it does in one
// sentence, because a bare verb name is not an affordance an LLM can
// choose between; Route must match a declared route exactly (method +
// absolute pattern), the same convention the composition root flattens
// with MountFeature. Params is the closed set of top-level form fields
// the verb forwards: an undeclared key is refused loud, naming the
// allowed names, instead of being silently dropped by the handler.
// ParamHelp documents those names for the agent; a key outside Params is
// structural drift and refused. Values stay strings on purpose — handlers
// parse values at the boundary, so typed coercion would be artificial
// precision until an action genuinely takes non-string input.
type InvokeAction struct {
	Verb        string   `yaml:"verb"`
	Description string   `yaml:"description,omitempty"`
	Route       Route    `yaml:"route"`
	Params      []string `yaml:"params,omitempty"`
	// PathParams names the route placeholders the caller must supply. v1 is
	// deliberately narrow: the repository's ids are UUIDs, so the only
	// accepted placeholder is `{id}`. A slug or a second placeholder is a
	// future format decision, not something the dispatcher should guess.
	PathParams []string          `yaml:"pathParams,omitempty"`
	ParamHelp  map[string]string `yaml:"paramHelp,omitempty"`
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
		problems = append(problems, actionProblemsFor(a, declared)...)
	}
	return problems
}

// actionProblemsFor validates one action against the declared route set. Split
// from the loop so each rule stays readable and the function complexity tracks
// the table, not the number of actions.
func actionProblemsFor(a InvokeAction, declared map[Route]bool) []string {
	var problems []string
	if !declared[a.Route] {
		problems = append(problems,
			fmt.Sprintf("action %q targets undeclared route %s %s",
				a.Verb, a.Route.Method, a.Route.Pattern))
	}
	params, paramProblems := actionParamProblems(a)
	problems = append(problems, paramProblems...)
	if !invocableMethod(a.Route.Method) {
		problems = append(problems,
			fmt.Sprintf("action %q uses method %q, which cannot be invoked headless", a.Verb, a.Route.Method))
	}
	if streamPath(a.Route.Pattern) {
		problems = append(problems,
			fmt.Sprintf("action %q targets streaming route %q, which would hang an invoke", a.Verb, a.Route.Pattern))
	}
	// Help is metadata, but a key outside the closed param set makes
	// discovery lie about the shape the handler accepts. Sorted so the
	// same declaration always reports the same problem order.
	for _, name := range slices.Sorted(maps.Keys(a.ParamHelp)) {
		if !params[name] {
			problems = append(problems,
				fmt.Sprintf("action %q documents param %q, which it does not declare", a.Verb, name))
		}
		if strings.TrimSpace(a.ParamHelp[name]) == "" {
			problems = append(problems,
				fmt.Sprintf("action %q has empty help for param %q", a.Verb, name))
		}
	}
	return problems
}

// actionParamProblems validates the closed body/path param sets and returns
// the union for the help check. `{id}` lives in PathParams but is still a
// param a caller supplies, so both lists share one duplicate detector.
func actionParamProblems(a InvokeAction) (map[string]bool, []string) {
	params := map[string]bool{}
	var problems []string
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
	problems = append(problems, pathParamProblems(a)...)
	for _, p := range a.PathParams {
		if params[p] {
			problems = append(problems, fmt.Sprintf(
				"action %q declares %q as both a body param and a path param", a.Verb, p))
		}
		params[p] = true
	}
	return params, problems
}

// ActionDocProblems is the review half of the action table: a verb with
// no description is a name, not an affordance, so an agent cannot choose
// it without reading the handler. The composition gate stays structural
// (ActionProblems) because an older recipe must keep booting; the catalog
// review and `recipe check` use this to demand the sentence a reviewer
// would otherwise have to reconstruct.
func ActionDocProblems(d Declaration) []string {
	var out []string
	for _, a := range d.Actions {
		if strings.TrimSpace(a.Description) == "" {
			out = append(out, fmt.Sprintf(
				"action %q has no description: an agent cannot tell what it does", a.Verb))
		}
	}
	return out
}

// OmittedRoute is a mutating route the author deliberately keeps out of the
// headless surface. The reason is required because an omission without one is
// indistinguishable from a route the author forgot to wire, which is exactly
// the failure `recipe check` exists to report.
type OmittedRoute struct {
	Route  Route  `yaml:"route"`
	Reason string `yaml:"reason"`
}

// OmissionProblems validates the deliberate-omission list against the same
// declared routes and actions. It is the second half of "every mutating route
// is either callable or explained": a route may not be both an action and an
// omission, and an omission may not name a route the declaration never claims.
func OmissionProblems(d Declaration) []string {
	declared := map[Route]bool{}
	for _, r := range d.Routes {
		declared[r] = true
	}
	acted := map[Route]bool{}
	for _, a := range d.Actions {
		acted[a.Route] = true
	}
	var out []string
	seen := map[Route]bool{}
	for _, o := range d.Omitted {
		if !declared[o.Route] {
			out = append(out, fmt.Sprintf(
				"omitted route %s %s is not declared", o.Route.Method, o.Route.Pattern))
		}
		if strings.TrimSpace(o.Reason) == "" {
			out = append(out, fmt.Sprintf(
				"omitted route %s %s has no reason: an unexplained omission is the gap the check reports",
				o.Route.Method, o.Route.Pattern))
		}
		if acted[o.Route] {
			out = append(out, fmt.Sprintf(
				"route %s %s is both an action and omitted", o.Route.Method, o.Route.Pattern))
		}
		if seen[o.Route] {
			out = append(out, fmt.Sprintf(
				"route %s %s is omitted more than once", o.Route.Method, o.Route.Pattern))
		}
		seen[o.Route] = true
		if !mutatingRouteMethod(o.Route.Method) {
			out = append(out, fmt.Sprintf(
				"route %s %s is omitted, but only mutating routes need an omission",
				o.Route.Method, o.Route.Pattern))
		}
	}
	return out
}

func mutatingRouteMethod(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
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

// pathParamProblems keeps the route placeholders and the declared path
// params in lockstep. The rule is closed on purpose: v1 supports exactly
// one `{id}` (the repository's UUID), so an action cannot advertise a
// placeholder the dispatcher will not fill, nor a body param that is really
// part of the path.
func pathParamProblems(a InvokeAction) []string {
	placeholders, ok := routePlaceholders(a.Route.Pattern)
	if !ok {
		return []string{fmt.Sprintf(
			"action %q route %s has a malformed path parameter", a.Verb, a.Route.Pattern)}
	}
	var out []string
	if len(placeholders) == 0 {
		if len(a.PathParams) > 0 {
			out = append(out, fmt.Sprintf(
				"action %q declares path params %v, but route %s has no placeholder",
				a.Verb, a.PathParams, a.Route.Pattern))
		}
		return out
	}
	if len(placeholders) != 1 || placeholders[0] != "id" {
		out = append(out, fmt.Sprintf(
			"action %q route %s: only one {id} placeholder is invocable headless",
			a.Verb, a.Route.Pattern))
	}
	seen := map[string]bool{}
	for _, p := range a.PathParams {
		if p != "id" {
			out = append(out, fmt.Sprintf(
				"action %q declares path param %q; v1 accepts only %q (a UUID)", a.Verb, p, "id"))
		}
		if p == "" {
			out = append(out, fmt.Sprintf("action %q has an empty path param name", a.Verb))
		}
		if seen[p] {
			out = append(out, fmt.Sprintf("action %q duplicates path param %q", a.Verb, p))
		}
		seen[p] = true
	}
	if len(a.PathParams) != 1 || a.PathParams[0] != "id" {
		out = append(out, fmt.Sprintf(
			"action %q route %s must declare pathParams: [id]", a.Verb, a.Route.Pattern))
	}
	return out
}

// routePlaceholders extracts the `{name}` names from a route pattern. ok is
// false when the pattern has a stray brace or an empty/malformed name, which
// is a declaration defect rather than an invocable route.
func routePlaceholders(pattern string) ([]string, bool) {
	var names []string
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			end := strings.IndexByte(pattern[i+1:], '}')
			if end < 0 {
				return nil, false
			}
			name := pattern[i+1 : i+1+end]
			if !validPathParamName(name) {
				return nil, false
			}
			names = append(names, name)
			i += end + 1
		case '}':
			return nil, false
		}
	}
	return names, true
}

func validPathParamName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
		digit := r >= '0' && r <= '9'
		if i == 0 && !letter {
			return false
		}
		if !letter && !digit {
			return false
		}
	}
	return true
}
