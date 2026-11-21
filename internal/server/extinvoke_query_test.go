package server

import (
	"net/http"
	"testing"

	"github.com/calionauta/gogogo/internal/recipe"
)

// A read verb puts its parameters in the query string, not the form body: a GET
// handler that parses a form sees nothing, so declaring `query` is what makes
// the dashboard's `scope` reachable. This is the dashboard's shape, proved on
// the endpoint rather than on the handler alone.
func TestInvokeEndpointPutsDeclaredQueryInTheURL(t *testing.T) {
	spy := &spyHandler{body: `{"scope":"all"}`}
	decl := recipe.Declaration{
		Routes: []recipe.Route{
			{Method: http.MethodGet, Pattern: "/probe/read"},
		},
		Actions: []recipe.InvokeAction{
			{
				Verb:        "read",
				Route:       recipe.Route{Method: http.MethodGet, Pattern: "/probe/read"},
				Query:       []string{"scope"},
				BrowserPath: "/probe",
			},
		},
	}
	h := extInvokeHandler(registryWith(decl), func() http.Handler { return spy })
	rec := callInvoke(t, h, "session-token",
		mustJSON(t, extInvokeRequest{ID: probeID, Verb: "read", Params: map[string]string{"scope": "all"}}))

	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status = %d (%s)", rec.Code, rec.Body.String())
	}
	if spy.method != http.MethodGet {
		t.Errorf("handler saw %s, want GET — a read must not become a write", spy.method)
	}
	if spy.path != "/probe/read" {
		t.Errorf("handler path = %q, want /probe/read", spy.path)
	}
	if got := spy.query.Get("scope"); got != "all" {
		t.Errorf("handler query scope = %q, want all (a declared query param must travel in the URL)", got)
	}
}
