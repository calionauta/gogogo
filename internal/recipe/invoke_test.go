package recipe

import (
	"context"
	"net/http"
	"net/url"
	"strings"
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

var probeCreate = InvokeAction{
	Verb:   "create",
	Route:  Route{Method: "POST", Pattern: "/api/probe"},
	Params: []string{"title"},
}

// TestInvokeAnonymousIs401WithoutReachingHandler is the auth bridge's
// load-bearing property: an empty token must never reach the handler.
func TestInvokeAnonymousIs401WithoutReachingHandler(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	code, _, err := Invoke(context.Background(), handler, "gogogo_auth", "",
		probeCreate, Route{Method: "POST", Pattern: "/api/probe"}, url.Values{})
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
	code, _, err := Invoke(context.Background(), handler, "gogogo_auth", "tok-123", probeCreate, gotRoute,
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

// TestInvokeRejectsUnknownParam pins the closed param set: a typo'd key is
// refused (naming it and the allowed set) before serving, instead of
// reaching the handler and being silently dropped.
func TestInvokeRejectsUnknownParam(t *testing.T) {
	route := Route{Method: "POST", Pattern: "/api/probe"}
	action := InvokeAction{Verb: "create", Route: route, Params: []string{"title"}}
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	_, _, err := Invoke(context.Background(), handler, "gogogo_auth", "tok",
		action, route, url.Values{"titl": {"typo"}, "title": {"ok"}})
	if err == nil {
		t.Fatal("undeclared param must be refused")
	}
	msg := err.Error()
	if !strings.Contains(msg, "titl") || !strings.Contains(msg, "title") {
		t.Fatalf("refusal must name the param and the allowed set, got %q", msg)
	}
	if called {
		t.Fatal("unknown param reached the handler")
	}
}

// TestInvokeServesReadAction proves the headless read path: a declared
// GET action is served once through the same bridge as a mutation.
func TestInvokeServesReadAction(t *testing.T) {
	route := Route{Method: "GET", Pattern: "/api/probe"}
	action := InvokeAction{Verb: "list", Route: route, Params: []string{"q"}}
	calls := 0
	var gotQuery string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotQuery = r.URL.Query().Get("q")
		w.WriteHeader(http.StatusOK)
	})
	code, _, err := Invoke(context.Background(), handler, "gogogo_auth", "tok",
		action, route, url.Values{})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if calls != 1 || gotQuery != "" {
		t.Fatalf("calls=%d q=%q", calls, gotQuery)
	}
}

// TestInvokeRefusesStreamingRoute pins the SSE boundary: a stream target
// must fail before serving — a recorder would hang forever.
func TestInvokeRefusesStreamingRoute(t *testing.T) {
	route := Route{Method: "GET", Pattern: "/api/probe/stream"}
	action := InvokeAction{Verb: "tail", Route: route}
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	_, _, err := Invoke(context.Background(), handler, "gogogo_auth", "tok",
		action, route, url.Values{})
	if err == nil {
		t.Fatal("streaming route must be refused")
	}
	if called {
		t.Fatal("streaming invoke reached the handler")
	}
}

// TestActionProblemsRejectDuplicateAndEmptyParam pins the declaration
// gate on the param set itself.
func TestActionProblemsRejectDuplicateAndEmptyParam(t *testing.T) {
	route := Route{Method: "POST", Pattern: "/api/probe"}
	d := Declaration{
		Routes:  []Route{route},
		Actions: []InvokeAction{{Verb: "create", Route: route, Params: []string{"title", "title", ""}}},
	}
	problems := ActionProblems(d)
	if len(problems) != 2 {
		t.Fatalf("duplicate + empty param must produce two problems, got %v", problems)
	}
}

// TestActionProblemsRejectNonInvocableMethod pins the method rule at the
// declaration gate: a verb on HEAD cannot be invoked headless.
func TestActionProblemsRejectNonInvocableMethod(t *testing.T) {
	route := Route{Method: "HEAD", Pattern: "/api/probe"}
	d := Declaration{
		Routes:  []Route{route},
		Actions: []InvokeAction{{Verb: "peek", Route: route}},
	}
	problems := ActionProblems(d)
	if len(problems) != 1 {
		t.Fatalf("HEAD verb must produce exactly one problem, got %v", problems)
	}
}

// TestActionProblemsRejectStreamingTarget pins the SSE rule at the
// declaration gate: a verb targeting a stream is refused.
func TestActionProblemsRejectStreamingTarget(t *testing.T) {
	route := Route{Method: "GET", Pattern: "/api/probe/stream"}
	d := Declaration{
		Routes:  []Route{route},
		Actions: []InvokeAction{{Verb: "tail", Route: route}},
	}
	problems := ActionProblems(d)
	if len(problems) != 1 {
		t.Fatalf("stream verb must produce exactly one problem, got %v", problems)
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
	_, _, err := Invoke(context.Background(), handler, "gogogo_auth", "tok", probeCreate,
		Route{Method: "POST", Pattern: "/api/probe/{id}/toggle"}, url.Values{})
	if err == nil {
		t.Fatal("parameterized route must be refused")
	}
	if called {
		t.Fatal("parameterized invoke reached the handler")
	}
}
