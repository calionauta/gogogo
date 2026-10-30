// SCOPE:plugin — Runtime dashboard handler tests.
package perf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestRoutesServeDashboardBehindFence pins the three routes the operator
// link depends on: the trailing-slash redirect, the index page, and the
// websocket endpoint (a plain GET is rejected by the upgrader, not 404).
// Red-proof: drop the wildcard route and the index assertion goes red.
func TestRoutesServeDashboardBehindFence(t *testing.T) {
	h, err := New()
	if err != nil {
		t.Fatalf("perf.New: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })

	var fenced int
	fence := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fenced++
			next.ServeHTTP(w, r)
		})
	}
	r := h.Routes(fence)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, Root+"/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Statsviz") {
		t.Error("index body is not the statsviz page")
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, Root, nil))
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("bare path status = %d, want 301", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != Root+"/" {
		t.Errorf("redirect Location = %q, want %q", loc, Root+"/")
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, Root+"/ws", nil))
	if rec.Code == http.StatusNotFound {
		t.Fatal("websocket route is not registered")
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/pprof/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("pprof index status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "goroutine") {
		t.Error("pprof index body is not the profile list")
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/debug/pprof", nil))
	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("bare pprof status = %d, want 301", rec.Code)
	}
	if fenced != 5 {
		t.Errorf("fence ran %d times, want 5", fenced)
	}
}

// TestRoutesRequireFence pins that a fence-less mount is refused: the
// dashboard would otherwise be a public pprof-adjacent surface.
func TestRoutesRequireFence(t *testing.T) {
	h, err := New()
	if err != nil {
		t.Fatalf("perf.New: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	defer func() {
		if recover() == nil {
			t.Error("Routes() without a fence did not panic")
		}
	}()
	h.Routes()
}

// TestRoutesUseRoot pins the duplication the lint needs: the registered paths
// are literals (so the RBAC coverage lint and the api-routes doc can read
// them) and must stay the constant the menu link names.
func TestRoutesUseRoot(t *testing.T) {
	h, err := New()
	if err != nil {
		t.Fatalf("perf.New: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	r := h.Routes(func(next http.Handler) http.Handler { return next })

	var got []string
	walk := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodGet {
			got = append(got, route)
		}
		return nil
	}
	if werr := chi.Walk(r, walk); werr != nil {
		t.Fatalf("walk routes: %v", werr)
	}
	want := []string{Root, Root + "/ws", Root + "/*", "/debug/pprof", "/debug/pprof/*"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("registered routes = %v, want %v", got, want)
	}
}
