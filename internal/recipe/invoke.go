// Headless invocation: the auth bridge.
//
// Discovery answers "what exists"; ResolveAction answers "which declared
// route does this verb name"; Invoke executes one resolved action
// against the composed mux in-process, under the operator-supplied
// session token.
//
// The bridge reuses the server's own auth instead of inventing a second
// one: the token becomes the ordinary session cookie, RequireSession
// validates it, and the handler's own session check runs as it does for
// a browser. An empty token never reaches the handler — the bridge
// answers 401 first, so a missing identity fails loud instead of
// becoming an anonymous write downstream.
package recipe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// ResolveAction maps (id, verb) to the declared route, or fails naming
// what was asked. Unknown id and unknown verb are different failures:
// the first means the extension is not here, the second means the verb
// is not invocable — both must fail before any request is built.
func ResolveAction(reg *Registry, id, verb string) (Route, error) {
	_, route, err := ResolveInvokeAction(reg, id, verb)
	return route, err
}

// ResolveInvokeAction is ResolveAction plus the action itself — the
// bridge needs its closed param set to reject unknown keys before
// dispatching.
func ResolveInvokeAction(reg *Registry, id, verb string) (InvokeAction, Route, error) {
	if reg == nil {
		return InvokeAction{}, Route{}, fmt.Errorf("recipe: no registry to resolve %q", id)
	}
	for _, p := range reg.Providers() {
		if p.ID() != id {
			continue
		}
		for _, a := range p.Declaration().Actions {
			if a.Verb == verb {
				return a, a.Route, nil
			}
		}
		return InvokeAction{}, Route{}, fmt.Errorf("recipe: extension %q declares no invocable action %q", id, verb)
	}
	return InvokeAction{}, Route{}, fmt.Errorf("recipe: unknown extension %q", id)
}

// Invoke serves one resolved action against handler in-process and
// returns the handler's status and body. cookieName is the session
// cookie as defined by the caller (the composition root passes
// auth.CookieName; recipe stays free of auth so every consumer keeps
// its current dependency set).
//
// An empty token is refused with 401 without reaching the handler.
// Invocable methods are GET (reads) and the mutating methods; streaming
// routes ({/stream, /events}) and HEAD are refused — a stream would
// hang an invoke forever. form carries the request body keys; keys
// outside the action's closed param set are refused loud, naming the
// allowed set, instead of being silently ignored. A declared `{id}` is
// filled from `id` in form and validated as a UUID before the URL is built;
// the id is removed from the form, so the handler sees it only as a route
// parameter. A placeholder the action did not declare is refused — the
// dispatcher never guesses an id.
func Invoke(
	ctx context.Context, handler http.Handler, cookieName, token string,
	action InvokeAction, route Route, form url.Values,
) (int, []byte, error) {
	if token == "" {
		return http.StatusUnauthorized, nil, errors.New("recipe: invoke requires a session")
	}
	if cookieName == "" {
		return http.StatusBadRequest, nil, errors.New("recipe: invoke requires a session cookie name")
	}
	if !invocableMethod(route.Method) {
		return http.StatusBadRequest, nil, fmt.Errorf(
			"recipe: %s %s cannot be invoked headless", route.Method, route.Pattern)
	}
	if streamPath(route.Pattern) {
		return http.StatusBadRequest, nil, fmt.Errorf(
			"recipe: %s %s streams forever and cannot be invoked headless",
			route.Method, route.Pattern)
	}
	if handler == nil {
		return http.StatusInternalServerError, nil, errors.New("recipe: invoke with a nil handler")
	}
	target, form, terr := invokeTarget(action, route, form)
	if terr != nil {
		return http.StatusBadRequest, nil, terr
	}
	if err := checkInvokeParams(action, form); err != nil {
		return http.StatusBadRequest, nil, err
	}
	req := httptest.NewRequestWithContext(
		ctx, route.Method, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{
		Name:     cookieName,
		Value:    token,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes(), nil
}

// invokeTarget fills the declared path placeholders and returns the URL the
// handler sees plus the form without those keys. v1 accepts only `{id}`, and
// only as a UUID: the repository generates UUIDv7 ids, and a free-form
// segment would make a typo look like a different record instead of a bad
// request. The id is canonicalized by uuid.Parse before it reaches chi.
func invokeTarget(action InvokeAction, route Route, form url.Values) (string, url.Values, error) {
	clean := url.Values{}
	for k, values := range form {
		clean[k] = append([]string(nil), values...)
	}
	placeholders, ok := routePlaceholders(route.Pattern)
	if !ok {
		return "", nil, fmt.Errorf(
			"recipe: %s %s has a malformed path parameter", route.Method, route.Pattern)
	}
	if len(placeholders) == 0 {
		if len(action.PathParams) > 0 {
			return "", nil, fmt.Errorf(
				"recipe: action %q declares path params %v, but %s has no placeholder",
				action.Verb, action.PathParams, route.Pattern)
		}
		return route.Pattern, clean, nil
	}
	if len(placeholders) != 1 || placeholders[0] != "id" {
		return "", nil, fmt.Errorf(
			"recipe: %s %s is parameterized beyond the supported {id}",
			route.Method, route.Pattern)
	}
	if len(action.PathParams) != 1 || action.PathParams[0] != "id" {
		return "", nil, fmt.Errorf(
			"recipe: action %q does not declare pathParams: [id] for %s",
			action.Verb, route.Pattern)
	}
	raw := clean.Get("id")
	if raw == "" {
		return "", nil, fmt.Errorf("recipe: action %q needs path param %q", action.Verb, "id")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf(
			"recipe: action %q path param %q must be a UUID: %w", action.Verb, "id", err)
	}
	clean.Del("id")
	target := strings.ReplaceAll(route.Pattern, "{id}", id.String())
	if strings.ContainsAny(target, "{}") {
		return "", nil, fmt.Errorf(
			"recipe: action %q left an unresolved path parameter in %s", action.Verb, target)
	}
	return target, clean, nil
}

// checkInvokeParams refuses any key the action does not declare.
// An empty closed set rejects every key (a no-input action fails loud
// on any input rather than guessing).
func checkInvokeParams(action InvokeAction, form url.Values) error {
	for k := range form {
		if !slices.Contains(action.Params, k) {
			return fmt.Errorf("recipe: %q allows params %v, not %q",
				action.Verb, action.Params, k)
		}
	}
	return nil
}
