// SCOPE:core — Route-table contract: flattening, duplicates, 405.
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestMountFeatureFlattensRoutes pins the composition contract: the root
// serves a feature's absolute patterns, its params and its own middleware —
// without the feature router ever being mounted (no prefix stripping).
func TestMountFeatureFlattensRoutes(t *testing.T) {
	feat := chi.NewRouter()
	feat.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Feature", "yes")
			next.ServeHTTP(w, r)
		})
	})
	var gotID string
	feat.Get("/things/{id}", func(_ http.ResponseWriter, r *http.Request) {
		gotID = chi.URLParam(r, "id")
	})

	root := NewRouter()
	root.MountFeature("things", feat)

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/things/42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /things/42: status = %d, want 200", rec.Code)
	}
	if gotID != "42" {
		t.Errorf("param not preserved: chi.URLParam = %q, want 42", gotID)
	}
	if got := rec.Header().Get("X-Feature"); got != "yes" {
		t.Errorf("feature middleware not preserved: X-Feature = %q, want yes", got)
	}
}

// TestMountFeatureDuplicatePanics pins the guard that makes the route table
// trustworthy: two capabilities claiming one method+pattern is a boot error
// naming both, never a silently overwritten handler.
func TestMountFeatureDuplicatePanics(t *testing.T) {
	route := func() chi.Router {
		r := chi.NewRouter()
		r.Get("/same", func(http.ResponseWriter, *http.Request) {})
		return r
	}
	root := NewRouter()
	root.MountFeature("first", route())

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("duplicate route did not panic")
		}
		msg, ok := rec.(string)
		if !ok {
			t.Fatalf("panic value = %T, want the owner-naming string", rec)
		}
		for _, want := range []string{"GET /same", "first", "second"} {
			if !strings.Contains(msg, want) {
				t.Errorf("panic %q does not name %q", msg, want)
			}
		}
	}()
	root.MountFeature("second", route())
}

// TestUnknownMethodAnswers405 pins the router-level method decision: a path
// the tree serves for GET is not a hand-rolled check in every handler, so a
// POST answers 405 instead of falling through to the landing page.
func TestUnknownMethodAnswers405(t *testing.T) {
	feat := chi.NewRouter()
	feat.Get("/only-get", func(http.ResponseWriter, *http.Request) {})

	root := NewRouter()
	root.MountFeature("only", feat)
	root.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/only-get", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /only-get: status = %d, want 405", rec.Code)
	}
}

// TestMountSubtreeKeepsFullPath pins MountSubtree's reason to exist: the
// handler trims its own prefix, so it must see the full request path. The auth
// handler and the asset handlers depend on it.
func TestMountSubtreeKeepsFullPath(t *testing.T) {
	var seen string
	h := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = r.URL.Path })

	root := NewRouter()
	MountSubtree(root, "/assets", h)

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/assets/x.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/x.js: status = %d, want 200", rec.Code)
	}
	if seen != "/assets/x.js" {
		t.Errorf("handler saw path %q, want /assets/x.js (prefix stripped)", seen)
	}
}

// TestUnknownPathReachesLanding pins the not-found delegation: the landing
// router is the catch-all, so an unmatched URL renders it rather than a bare
// 404.
func TestUnknownPathReachesLanding(t *testing.T) {
	root := NewRouter()
	root.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("landing"))
	})

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "landing" {
		t.Fatalf("GET /nope = %d %q, want 200 landing", rec.Code, rec.Body.String())
	}
}

// TestMountFeatureNilIsNoop pins the disabled-capability path: a feature that
// answers nil from Routes() mounts no route and boots without a panic.
func TestMountFeatureNilIsNoop(t *testing.T) {
	root := NewRouter()
	root.MountFeature("disabled", nil)

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/anything", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /anything: status = %d, want 404", rec.Code)
	}
}

// TestRecoveryMiddlewareAnswers500 pins the panic fence: a handler panic must
// answer 500 through the composed tree, not drop the connection (which is what
// a streaming SSE client would otherwise see as a dead stream).
func TestRecoveryMiddlewareAnswers500(t *testing.T) {
	feat := chi.NewRouter()
	feat.Get("/boom", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	root := NewRouter()
	root.Use(recoveryMiddleware)
	root.MountFeature("boom", feat)

	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/boom", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /boom: status = %d, want 500", rec.Code)
	}
}
