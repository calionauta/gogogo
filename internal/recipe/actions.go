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
	// PathParams names the route placeholders the caller must supply. The
	// names are the handler's own (`id`, `docID`, …) and must match the
	// pattern's placeholders exactly; the dispatcher still refuses a value
	// that is not a UUID, so a typo fails as a bad request instead of
	// addressing a different record.
	PathParams []string `yaml:"pathParams,omitempty"`
	// Query names params the handler reads from the URL query string rather
	// than the body (`?clientID=`). Without it a route whose handler reads
	// the query would be invocable only as a half-call, so the declaration
	// says where each param travels instead of the bridge guessing.
	Query []string `yaml:"query,omitempty"`
	// RawBody names the one param whose value is sent verbatim as the
	// request body with Content-Type application/json. Handlers that decode
	// a nested JSON document cannot be reached with flat form fields; a raw
	// body keeps the closed param set while letting the caller supply the
	// document the handler already documents. With RawBody set, every other
	// param must travel in the path or the query.
	RawBody string `yaml:"rawBody,omitempty"`
	// BrowserPath is the page a human clicks to do the same thing (`/notes/new`
	// for the `create` action). It exists because a create page is a GET that
	// mints a record and answers with a redirect — a shape the coverage rule
	// cannot see and a headless caller cannot use. Naming it here binds the two
	// faces of one capability: the GET must be declared, and every GET create
	// page must be bound to an action (see browserAliasProblems).
	BrowserPath string            `yaml:"browserPath,omitempty"`
	ParamHelp   map[string]string `yaml:"paramHelp,omitempty"`
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
	aliased := map[Route]bool{}
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
		if a.BrowserPath != "" {
			alias := Route{Method: "GET", Pattern: a.BrowserPath}
			if aliased[alias] {
				problems = append(problems, fmt.Sprintf(
					"action %q shares browser page %s with another action", a.Verb, a.BrowserPath))
			}
			aliased[alias] = true
		}
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
	problems = append(problems, actionLocationProblems(a, params)...)
	problems = append(problems, browserPathProblems(a, declared)...)
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

// browserPathProblems keeps the human page and the headless verb pointed at
// the same capability: the page must be a declared GET route (so a typo cannot
// invent one) and must not be the action's own route (a GET cannot be both the
// headless target and the browser alias).
func browserPathProblems(a InvokeAction, declared map[Route]bool) []string {
	if a.BrowserPath == "" {
		return nil
	}
	page := Route{Method: "GET", Pattern: a.BrowserPath}
	var out []string
	if !declared[page] {
		out = append(out, fmt.Sprintf(
			"action %q browserPath %s is not a declared GET route", a.Verb, a.BrowserPath))
	}
	if a.BrowserPath == a.Route.Pattern {
		out = append(out, fmt.Sprintf(
			"action %q browserPath is its own route %s", a.Verb, a.BrowserPath))
	}
	return out
}

// actionParamProblems validates the closed body/path param sets and returns
// the union for the help check. Path and query params are still params a
// caller supplies, so every list shares one duplicate detector.
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
	for _, p := range a.Query {
		switch {
		case p == "":
			problems = append(problems, fmt.Sprintf("action %q has an empty query param name", a.Verb))
		case params[p]:
			problems = append(problems, fmt.Sprintf(
				"action %q declares %q in more than one place", a.Verb, p))
		}
		params[p] = true
	}
	return params, problems
}

// actionLocationProblems keeps the raw body honest: the body slot holds one
// document, so every other declared param must travel in the path or the query
// (otherwise the bridge would have nowhere to put it), and the raw-body param
// must be a declared param so the closed set still bounds what is forwarded.
func actionLocationProblems(a InvokeAction, _ map[string]bool) []string {
	if a.RawBody == "" {
		return nil
	}
	var out []string
	if !slices.Contains(a.Params, a.RawBody) {
		out = append(out, fmt.Sprintf(
			"action %q rawBody %q is not one of its params", a.Verb, a.RawBody))
	}
	moved := map[string]bool{}
	for _, p := range a.PathParams {
		moved[p] = true
	}
	for _, p := range a.Query {
		moved[p] = true
	}
	for _, p := range a.Params {
		if p != a.RawBody && !moved[p] {
			out = append(out, fmt.Sprintf(
				"action %q sends a raw body, so param %q must be a path or query param", a.Verb, p))
		}
	}
	return out
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
