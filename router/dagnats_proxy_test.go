// SCOPE:layer=feature,removal=plugin — verifies the DagNats console proxy
// forwards every HTTP method, not just GET.
//
// Regression guard for the bug where the console rendered fine but every write
// failed: the proxy registered only `GET /dagnats/{path...}`, so a POST (create
// a trigger, edit a workflow, cancel a run) never matched a route and
// PocketBase answered 404. The upstream DagNats mux registers its handlers with
// `mux.Handle` (method-agnostic), so the correct proxy registration is `Any`.
package router_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	approuter "github.com/calionauta/gogogo-fullstack-template/router"

	_ "github.com/ncruces/go-sqlite3/driver"
)

type stubCalls struct {
	method, path, body string
	seen               bool
}

func (c *stubCalls) add(method, path, body string) {
	c.method, c.path, c.body, c.seen = method, path, body, true
}

// stubUpstream records the method and path it received, and echoes them back.
// Standing in for the DagNats engine keeps the test hermetic — it asserts the
// proxy's routing behaviour, which is what broke, not DagNats' own logic.
func stubUpstream(t *testing.T) (*httptest.Server, *stubCalls) {
	t.Helper()
	calls := &stubCalls{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls.add(r.Method, r.URL.Path, string(body))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"method": r.Method,
			"path":   r.URL.Path,
			"body":   string(body),
		})
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

// proxyFixture builds the mux the same way router.Init does — a PocketBase
// router with the DagNats proxy mounted — and serves it over httptest.
//
// The router is constructed directly rather than through app.OnServe():
// OnServe only fires during app.Start(), and these tests assert routing, so
// they do not need the HTTP server or the hook chain.
func proxyFixture(t *testing.T, upstream string) *httptest.Server {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "dagnats-proxy-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  tmpDir,
		HideStartBanner: true,
	})

	r := router.NewRouter[*core.RequestEvent](
		func(w http.ResponseWriter, req *http.Request) (*core.RequestEvent, router.EventCleanupFunc) {
			e := &core.RequestEvent{App: app}
			e.Response = w
			e.Request = req
			return e, nil
		},
	)
	approuter.MountDagNatsDashboardForTest(
		&core.ServeEvent{App: app, Router: r},
		upstream,
	)

	mux, err := r.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestDagNatsProxyForwardsEveryMethod is the red-proof: with the proxy
// registered as `GET` only, every method below except GET returns 404 and the
// test fails. With `Any`, all of them reach the upstream.
func TestDagNatsProxyForwardsEveryMethod(t *testing.T) {
	t.Parallel()

	upstream, calls := stubUpstream(t)
	proxy := proxyFixture(t, upstream.URL)

	// The console is a CRUD app: it creates triggers, edits workflows,
	// cancels and retries runs, and deletes schedules. Each of these is a
	// distinct method that the old GET-only proxy silently 404'd.
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/dagnats/console/", ""},
		{http.MethodPost, "/dagnats/console/api/triggers", `{"name":"t1"}`},
		{http.MethodPut, "/dagnats/console/api/workflows/wf1", `{"name":"wf1"}`},
		{http.MethodPatch, "/dagnats/console/api/runs/run1", `{"action":"cancel"}`},
		{http.MethodDelete, "/dagnats/console/api/triggers/t1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			*calls = stubCalls{} // reset per case

			req, err := http.NewRequestWithContext(t.Context(), tc.method, proxy.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := proxy.Client().Do(req)
			if err != nil {
				t.Fatalf("%s %s: %v", tc.method, tc.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				t.Fatalf(
					"%s %s -> 404: the proxy did not match this method. "+
						"Register it with Router.Any, not Router.GET.",
					tc.method, tc.path,
				)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s %s -> %d, want 200", tc.method, tc.path, resp.StatusCode)
			}

			if !calls.seen {
				t.Fatalf("%s %s: upstream never received the request", tc.method, tc.path)
			}
			if calls.method != tc.method {
				t.Errorf("upstream saw method %s, want %s", calls.method, tc.method)
			}
			// The /dagnats prefix must be stripped on the way in, and the
			// body must survive the hop (POST/PUT/PATCH carry the payload).
			if want := strings.TrimPrefix(tc.path, "/dagnats"); calls.path != want {
				t.Errorf("upstream saw path %q, want %q", calls.path, want)
			}
			if calls.body != tc.body {
				t.Errorf("upstream saw body %q, want %q", calls.body, tc.body)
			}
		})
	}
}

// TestDagNatsProxyKeepsGetRendering guards the original behaviour: the console
// must still load, and the proxy must still rewrite absolute SPA paths so the
// assets resolve under /dagnats/.
func TestDagNatsProxyKeepsGetRendering(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<script src="/console/app.js"></script>`))
	}))
	t.Cleanup(srv.Close)

	proxy := proxyFixture(t, srv.URL)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, proxy.URL+"/dagnats/console/", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := proxy.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /dagnats/console/ -> %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `src="/dagnats/console/app.js"`) {
		t.Errorf("absolute SPA path was not rewritten to /dagnats: %s", body)
	}
}
