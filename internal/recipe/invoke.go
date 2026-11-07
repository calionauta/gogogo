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
// allowed set, instead of being silently ignored. Routes with path
// parameters ({id}) are refused — Invoke is literal-path only, so a
// parameterized target can never be called with a guessed id.
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
	if strings.Contains(route.Pattern, "{") {
		return http.StatusBadRequest, nil, fmt.Errorf(
			"recipe: %s %s is parameterized and not invocable headless",
			route.Method, route.Pattern)
	}
	if handler == nil {
		return http.StatusInternalServerError, nil, errors.New("recipe: invoke with a nil handler")
	}
	if form == nil {
		form = url.Values{}
	}
	if err := checkInvokeParams(action, form); err != nil {
		return http.StatusBadRequest, nil, err
	}
	req := httptest.NewRequestWithContext(
		ctx, route.Method, route.Pattern, strings.NewReader(form.Encode()))
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
