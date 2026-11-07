package recipe

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"
)

type invokeProvider struct {
	id     string
	routes []Route
	verbs  []InvokeAction
}

func (p invokeProvider) ID() string { return p.id }

func (p invokeProvider) Declaration() Declaration {
	return Declaration{Routes: p.routes, Actions: p.verbs}
}

func (p invokeProvider) Routes() chi.Router { return nil }

// TestInvokeAnonymousIs401WithoutReachingHandler is the auth bridge's
// load-bearing property: an empty token must never reach the handler.
func TestInvokeAnonymousIs401WithoutReachingHandler(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	code, _, err := Invoke(context.Background(), handler, "gogogo_auth", "",
		Route{Method: "POST", Pattern: "/api/probe"}, url.Values{})
	if err == nil {
		t.Fatal("empty token must error, not serve")
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("empty token must be 401, got %d", code)
	}
	if called {
		t.Fatal("anonymous invoke reached the handler")
	}
}

// TestInvokeUnknownVerbFailsBeforeServing proves resolution fails loud
// before any request exists.
func TestInvokeUnknownVerbFailsBeforeServing(t *testing.T) {
	route := Route{Method: "POST", Pattern: "/api/probe"}
	reg := &Registry{}
	reg.Add(invokeProvider{id: "probe", routes: []Route{route}, verbs: []InvokeAction{{Verb: "create", Route: route}}})
	if _, err := ResolveAction(reg, "probe", "explode"); err == nil {
		t.Fatal("undeclared verb must not resolve")
	}
	if _, err := ResolveAction(reg, "ghost", "create"); err == nil {
		t.Fatal("unknown extension must not resolve")
	}
	if _, err := ResolveAction(nil, "probe", "create"); err == nil {
		t.Fatal("nil registry must fail, not serve")
	}
}

// TestInvokeServesDeclaredRouteWithSession proves the happy path: a
// resolved route is served once, with the session cookie present.
func TestInvokeServesDeclaredRouteWithSession(t *testing.T) {
	route := Route{Method: "POST", Pattern: "/api/probe"}
	reg := &Registry{}
	reg.Add(invokeProvider{id: "probe", routes: []Route{route}, verbs: []InvokeAction{{Verb: "create", Route: route}}})
	gotRoute, err := ResolveAction(reg, "probe", "create")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if gotRoute != route {
		t.Fatalf("resolved %v, want %v", gotRoute, route)
	}
	calls := 0
	var cookie string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		cookie = ""
		if c, cerr := r.Cookie("gogogo_auth"); cerr == nil {
			cookie = c.Value
		}
		w.WriteHeader(http.StatusOK)
	})
	code, _, err := Invoke(context.Background(), handler, "gogogo_auth", "tok-123", gotRoute,
		url.Values{"title": {"Ship it"}})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("invoke status %d, want 200", code)
	}
	if calls != 1 {
		t.Fatalf("handler called %d times, want 1", calls)
	}
	if cookie != "tok-123" {
		t.Fatalf("session cookie missing, got %q", cookie)
	}
}

// TestInvokeRefusesParameterizedRoute pins the literal-path boundary: a
// {id} target must fail before the handler runs, never with a guessed id.
func TestInvokeRefusesParameterizedRoute(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	_, _, err := Invoke(context.Background(), handler, "gogogo_auth", "tok",
		Route{Method: "POST", Pattern: "/api/probe/{id}/toggle"}, url.Values{})
	if err == nil {
		t.Fatal("parameterized route must be refused")
	}
	if called {
		t.Fatal("parameterized invoke reached the handler")
	}
}
