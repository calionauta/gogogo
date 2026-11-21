// The headless-coverage rule: "every mutating route is callable or explained".
//
// Split from actions.go for the file-size gate. The rule lives beside the
// action and omission validators on purpose (actions.go keeps those), because
// the catalog review, `recipe check` and the strict CI gate share one
// definition: a mutating route that is neither an action's target nor a
// declared omission is the gap the advisory warning used to report — the slice
// ships a mutation no agent can reach.
package recipe

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// HeadlessCoverageProblems is the one rule that makes "every mutating route is
// callable or explained" verifiable: each mutating route the declaration claims
// must be either an action's target or a deliberate omission with a reason.
//
// It lives here, beside ActionProblems and OmissionProblems, so the catalog
// review, `recipe check` and the strict CI gate share one definition. A route
// that is neither is exactly the gap the advisory warning used to report: the
// slice ships a mutation no agent can reach.
//
// Exempt by construction, never by omission: streaming routes never complete
// (and `streamPath` refuses them as actions), wildcard patterns cannot name one
// record, and the auth entry points are the session's own bootstrap.
func HeadlessCoverageProblems(d Declaration) []string {
	invisible := HeadlessInvisibleRoutes(d)
	aliases := browserAliasProblems(d)
	out := make([]string, 0, len(invisible)+len(aliases))
	for _, r := range invisible {
		out = append(out, fmt.Sprintf(
			"%s %s mutates but is neither an action nor a declared omission: "+
				"name the verb an agent calls, or the reason it cannot be called",
			r.Method, r.Pattern))
	}
	for _, r := range aliases {
		out = append(out, fmt.Sprintf(
			"GET %s mints a record but no action names it as browserPath: "+
				"a browser-only create path has no headless twin",
			r.Pattern))
	}
	return out
}

// browserAliasProblems names the create pages no action claims. A GET whose
// last segment is `new` is the template's create-page convention: it writes a
// row and answers with a redirect, so the coverage rule (which reads mutating
// methods) never sees the write and a headless caller never receives the id.
// Binding it to the action's BrowserPath is what makes "every capability has a
// headless entry point" checkable for that shape too.
func browserAliasProblems(d Declaration) []Route {
	aliased := map[Route]bool{}
	for _, a := range d.Actions {
		if a.BrowserPath != "" {
			aliased[Route{Method: http.MethodGet, Pattern: a.BrowserPath}] = true
		}
	}
	var out []Route
	for _, r := range d.Routes {
		if r.Method != http.MethodGet || !createPagePath(r.Pattern) || aliased[r] {
			continue
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Route) int { return strings.Compare(a.Pattern, b.Pattern) })
	return out
}

// createPagePath is the create-page convention (`/notes/new`,
// `/whiteboard/new`): the whole path or its last segment is `new`.
func createPagePath(pattern string) bool {
	return pattern == "/new" || strings.HasSuffix(pattern, "/new")
}

// HeadlessInvisibleRoutes names the mutating routes neither an action nor an
// omission covers, sorted so the same declaration reports the same order. The
// evaluator and the catalog both read this list; the sentence built around it
// is the caller's.
func HeadlessInvisibleRoutes(d Declaration) []Route {
	acted := map[Route]bool{}
	for _, a := range d.Actions {
		acted[a.Route] = true
	}
	omitted := map[Route]bool{}
	for _, o := range d.Omitted {
		omitted[o.Route] = true
	}
	var out []Route
	for _, r := range d.Routes {
		if !mutatingRouteMethod(r.Method) || !headlessCandidate(r) {
			continue
		}
		if acted[r] || omitted[r] {
			continue
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Route) int {
		if c := strings.Compare(a.Method, b.Method); c != 0 {
			return c
		}
		return strings.Compare(a.Pattern, b.Pattern)
	})
	return out
}

// headlessCandidate reports whether a route could be an action at all: not a
// stream, not a wildcard, not an auth bootstrap.
func headlessCandidate(r Route) bool {
	return !streamPath(r.Pattern) && !strings.Contains(r.Pattern, "*") && !isAuthRoute(r.Pattern)
}

// authRoutes are the session bootstrap paths: their mutations are the login
// flow itself, so an action would be calling the door a key.
var authRoutes = []string{"/login", "/signup", "/logout", "/api/auth/"}

func isAuthRoute(pattern string) bool {
	return slices.Contains(authRoutes, pattern) || strings.HasPrefix(pattern, "/api/auth/")
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

// pathParamProblems keeps the route placeholders and the declared path params
// in lockstep. The names are the handler's own, but the set must match
// exactly: an action may not advertise a placeholder the dispatcher cannot
// fill, nor omit one the handler needs. The value rule (every placeholder is
// a UUID) lives in the dispatcher, where the id is actually parsed.
func pathParamProblems(a InvokeAction) []string {
	placeholders, ok := routePlaceholders(a.Route.Pattern)
	if !ok {
		return []string{fmt.Sprintf(
			"action %q route %s has a malformed path parameter", a.Verb, a.Route.Pattern)}
	}
	want := map[string]bool{}
	for _, p := range placeholders {
		want[p] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range a.PathParams {
		if p == "" {
			out = append(out, fmt.Sprintf("action %q has an empty path param name", a.Verb))
		}
		if seen[p] {
			out = append(out, fmt.Sprintf("action %q duplicates path param %q", a.Verb, p))
		}
		seen[p] = true
		if !want[p] {
			out = append(out, fmt.Sprintf(
				"action %q declares path param %q, but route %s has no such placeholder",
				a.Verb, p, a.Route.Pattern))
		}
	}
	for _, p := range placeholders {
		if !seen[p] {
			out = append(out, fmt.Sprintf(
				"action %q route %s placeholders must all be declared in pathParams (missing %q)",
				a.Verb, a.Route.Pattern, p))
		}
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
