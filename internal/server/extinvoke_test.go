// The invoke endpoint's own job, and nothing else.
//
// `POST /api/ext/invoke` is an HTTP envelope: decode the request, require the
// session cookie, resolve (id, verb) against the composed registry, hand the
// action to `recipe.Invoke`, and write back what came out. The resolve/serve
// semantics — param allowlisting, `{id}` UUID validation, streaming refusal —
// are `internal/recipe`'s contract and are proved there (invoke_test.go).
//
// These tests therefore drive the handler with a stub registry and a spy outer
// handler: no slice, no database, no policy, no boot. The reference slice used
// to be the fixture here, which meant a product that dropped it could not run
// its own server suite. The spy is enough to prove the envelope because the spy
// IS "the handler the action reaches".
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/recipe"
)

// probeID is the stub unit the endpoint resolves against. It is a name no
// recipe in the catalog uses, so a failure reads as "the endpoint forgot to
// resolve" rather than as a slice that happens to be missing.
const probeID = "endpoint-probe"

// probeProvider is the smallest Provider: one declaration, a router the seam
// would mount. The endpoint never mounts it — it only reads the declaration.
type probeProvider struct{ decl recipe.Declaration }

func (probeProvider) ID() string                        { return probeID }
func (p probeProvider) Declaration() recipe.Declaration { return p.decl }
func (probeProvider) Routes() chi.Router                { return chi.NewRouter() }

// probeActions names one verb per dispatch shape the endpoint forwards:
// a body-only write and a declared `{id}` path parameter.
var probeActions = []recipe.InvokeAction{
	{Verb: "create", Route: recipe.Route{Method: http.MethodPost, Pattern: "/probe"}, Params: []string{"title"}},
	{
		Verb:       "toggle",
		Route:      recipe.Route{Method: http.MethodPost, Pattern: "/probe/{id}/toggle"},
		PathParams: []string{"id"},
	},
}

func probeRegistry() *recipe.Registry {
	reg := &recipe.Registry{}
	reg.Add(probeProvider{decl: recipe.Declaration{Actions: probeActions}})
	return reg
}

// registryWith is probeRegistry with a caller-supplied declaration, for a verb
// whose shape the fixture above does not already cover (a GET read that carries
// its parameters in the query string, for one).
func registryWith(decl recipe.Declaration) *recipe.Registry {
	reg := &recipe.Registry{}
	reg.Add(probeProvider{decl: decl})
	return reg
}

// spyHandler records the request the action reached and answers with a body the
// test can recognise. It is the only handler in the chain, so "the spy saw it"
// is exactly "the declared action reached a handler".
type spyHandler struct {
	called bool
	method string
	path   string
	form   map[string][]string
	query  url.Values
	cookie string
	status int
	body   string
}

func (s *spyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.called = true
	s.method = r.Method
	s.path = r.URL.Path
	if err := r.ParseForm(); err == nil {
		s.form = r.PostForm
	}
	s.query = r.URL.Query()
	if c, err := r.Cookie(auth.CookieName); err == nil {
		s.cookie = c.Value
	}
	status := s.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write([]byte(s.body))
}

// invokeEndpoint builds the endpoint over the stub registry and the spy.
func invokeEndpoint(spy *spyHandler) http.HandlerFunc {
	return extInvokeHandler(probeRegistry(), func() http.Handler { return spy })
}

// callInvoke drives one request, with an empty token meaning "no cookie".
func callInvoke(t *testing.T, h http.Handler, token, payload string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(),
		http.MethodPost, "/api/ext/invoke", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		// #nosec G124 — test request cookie: the attributes are the server's
		// concern; the session token is the transport under test.
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: token, HttpOnly: true, Secure: true})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

// TestInvokeEndpointAnonymousNeverReachesAHandler is the fail-closed edge: no
// cookie is a 401 before resolution, so no handler runs.
func TestInvokeEndpointAnonymousNeverReachesAHandler(t *testing.T) {
	spy := &spyHandler{}
	rec := callInvoke(t, invokeEndpoint(spy), "",
		mustJSON(t, extInvokeRequest{ID: probeID, Verb: "create", Params: map[string]string{"title": "Anon"}}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous invoke status = %d, want 401", rec.Code)
	}
	if spy.called {
		t.Fatal("anonymous invoke reached the handler")
	}
}

// TestInvokeEndpointRunsDeclaredVerb proves the happy path through the
// envelope: resolve, forward the declared method and body params, and write the
// handler's status and body back as inert text.
func TestInvokeEndpointRunsDeclaredVerb(t *testing.T) {
	spy := &spyHandler{body: "created"}
	rec := callInvoke(t, invokeEndpoint(spy), "session-token",
		mustJSON(t, extInvokeRequest{ID: probeID, Verb: "create", Params: map[string]string{"title": "Headless"}}))
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status = %d (%s)", rec.Code, rec.Body.String())
	}
	if !spy.called {
		t.Fatal("the declared action never reached a handler")
	}
	if spy.method != http.MethodPost || spy.path != "/probe" {
		t.Fatalf("handler saw %s %s, want POST /probe", spy.method, spy.path)
	}
	if got := spy.form["title"]; len(got) != 1 || got[0] != "Headless" {
		t.Fatalf("handler form title = %v, want [Headless]", got)
	}
	if spy.cookie != "session-token" {
		t.Fatalf("handler cookie = %q, want the caller's session", spy.cookie)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain so reflected markup stays inert", got)
	}
	if rec.Body.String() != "created" {
		t.Fatalf("body = %q, want the handler's output", rec.Body.String())
	}
}

// TestInvokeEndpointFillsDeclaredPathID proves the one shape the endpoint adds
// on top of a body: a declared `{id}` is moved out of the form and into the
// URL, so the handler sees it only as a route parameter.
func TestInvokeEndpointFillsDeclaredPathID(t *testing.T) {
	const id = "018f5b8c-0000-7000-8000-000000000001"
	spy := &spyHandler{}
	rec := callInvoke(t, invokeEndpoint(spy), "session-token",
		mustJSON(t, extInvokeRequest{ID: probeID, Verb: "toggle", Params: map[string]string{"id": id}}))
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle status = %d (%s)", rec.Code, rec.Body.String())
	}
	if spy.path != "/probe/"+id+"/toggle" {
		t.Fatalf("handler path = %q, want the id in the URL", spy.path)
	}
	if _, leaked := spy.form["id"]; leaked {
		t.Fatal("the path id leaked into the form: the handler must see it only as a route parameter")
	}
}

// TestInvokeEndpointRefusesUndeclaredVerb proves the refusal direction: a verb
// the declaration never named is a 400 naming it, before any handler runs.
func TestInvokeEndpointRefusesUndeclaredVerb(t *testing.T) {
	spy := &spyHandler{}
	rec := callInvoke(t, invokeEndpoint(spy), "session-token",
		mustJSON(t, extInvokeRequest{ID: probeID, Verb: "explode"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("undeclared verb status = %d (%s), want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "explode") {
		t.Fatalf("refusal must name the verb, got %q", rec.Body.String())
	}
	if spy.called {
		t.Fatal("an undeclared verb reached the handler")
	}
}

// TestInvokeEndpointForwardsTheDispatchRefusal pins that a dispatch error keeps
// the status it came with instead of flattening to 500.
func TestInvokeEndpointForwardsTheDispatchRefusal(t *testing.T) {
	spy := &spyHandler{}
	rec := callInvoke(t, invokeEndpoint(spy), "session-token",
		mustJSON(t, extInvokeRequest{ID: probeID, Verb: "toggle", Params: map[string]string{"id": "not-a-uuid"}}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed id status = %d (%s), want the dispatch's 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "UUID") {
		t.Fatalf("refusal must name the UUID rule, got %q", rec.Body.String())
	}
	if spy.called {
		t.Fatal("a refused dispatch reached the handler")
	}
}

// TestInvokeEndpointRejectsUnknownFields pins the decoder's strictness: a
// request the product API could not honour is refused, not partially read.
func TestInvokeEndpointRejectsUnknownFields(t *testing.T) {
	rec := callInvoke(t, invokeEndpoint(&spyHandler{}), "session-token",
		`{"id":"`+probeID+`","verb":"create","surprise":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", rec.Code)
	}
}
