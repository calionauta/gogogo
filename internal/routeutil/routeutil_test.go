// SCOPE:layer=infra,removal=core — tests for the shared route registration.
//
// These pin the invariant that reached production as an eleven-hour crash
// loop: any pattern registered for every method must not be method-less, or
// Go's ServeMux panics when it is combined with the app's own `GET /`.
package routeutil_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/calionauta/gogogo/internal/routeutil"
)

// newRouter builds a router with the real request adapter, so BuildMux
// exercises the same ServeMux registration path production uses.
func newRouter() *router.Router[*core.RequestEvent] {
	return router.NewRouter[*core.RequestEvent](
		func(w http.ResponseWriter, r *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
			return &core.RequestEvent{Response: w, Request: r}, nil
		},
	)
}

// TestRegisterAllCoexistsWithRootRoute is the regression guard. Before the
// helper existed, the call sites used Router.Any(), which registers an empty
// method; combined with `GET /` that panics at BuildMux time:
//
//	pattern "GET /" conflicts with pattern "/dagnats/{path...}":
//	GET / matches fewer methods than /dagnats/{path...}, but has a more
//	general path pattern
//
// The panic happens during route registration, so it kills the process on
// boot rather than failing a request. Building the mux is therefore the
// assertion that matters.
func TestRegisterAllCoexistsWithRootRoute(t *testing.T) {
	r := newRouter()

	// The app's own root route, exactly as features/landing registers it.
	r.GET("/", func(*core.RequestEvent) error { return nil })

	routeutil.RegisterAll(r, []string{"/dagnats", "/dagnats/{path...}"},
		func(*core.RequestEvent) error { return nil })

	if _, err := r.BuildMux(); err != nil {
		t.Fatalf("registering per-method routes must not conflict with GET /: %v", err)
	}
}

// TestAnyWouldPanic documents the failure this helper exists to prevent. It
// asserts that the naive alternative really does break the mux, so the reason
// for the helper's existence is executable rather than a comment someone can
// decide is outdated.
func TestAnyWouldPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Router.Any() to panic when combined with GET /; " +
				"if this no longer panics, the helper may be unnecessary")
		}
	}()

	r := newRouter()
	r.GET("/", func(*core.RequestEvent) error { return nil })
	r.Any("/some-proxy/{path...}", func(*core.RequestEvent) error { return nil })
	_, _ = r.BuildMux() // expected to panic
}

// TestRegisterAllCoversEveryMethod proves the helper registers the verbs a
// console or API actually sends, not just GET. The original bug was a GET-only
// proxy silently 404ing every write.
func TestRegisterAllCoversEveryMethod(t *testing.T) {
	var seen []string
	r := newRouter()
	routeutil.RegisterAll(r, []string{"/proxy/{path...}"},
		func(e *core.RequestEvent) error {
			seen = append(seen, e.Request.Method)
			e.Response.WriteHeader(http.StatusOK)
			return nil
		})

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, method := range routeutil.Methods {
		req, err := http.NewRequestWithContext(
			t.Context(), method, srv.URL+"/proxy/anything", strings.NewReader(""))
		if err != nil {
			t.Fatalf("NewRequest(%s): %v", method, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s reached no handler (status %d)", method, resp.StatusCode)
		}
	}

	if len(seen) != len(routeutil.Methods) {
		t.Errorf("handler saw %d methods, want %d: %v",
			len(seen), len(routeutil.Methods), seen)
	}
}

// TestMethodsAreNotMethodless guards the property directly: Methods must never
// contain an empty string, because an empty method is what makes a pattern
// method-less and therefore conflicting.
func TestMethodsAreNotMethodless(t *testing.T) {
	if len(routeutil.Methods) == 0 {
		t.Fatal("Methods is empty; RegisterAll would register nothing")
	}
	for _, m := range routeutil.Methods {
		if strings.TrimSpace(m) == "" {
			t.Fatal("Methods contains an empty method, which produces a " +
				"method-less pattern and a startup panic")
		}
	}
	// Read verbs must be present for pages, and the mutating verbs for the
	// console/API writes that the original bug silently dropped.
	need := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete,
	}
	have := make(map[string]bool, len(routeutil.Methods))
	for _, m := range routeutil.Methods {
		have[m] = true
	}
	for _, m := range need {
		if !have[m] {
			t.Errorf("Methods is missing %s", m)
		}
	}
}
