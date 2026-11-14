// The api shape's runtime contract: page paths answer 404, API/health/assets
// keep working. Red-proof: remove applyShapeGate from buildChiMux (or the
// middleware from the gate) and the page assertions go green with 200.
package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/calionauta/gogogo/config"
)

func TestAPIShapeServesAPIAndHidesPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/todo", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/api/todos", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/static/app.min.css", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	h := apiOnlyPages(mux)
	cases := []struct {
		path string
		want int
	}{
		{"/todo", http.StatusNotFound},
		{"/dashboard", http.StatusNotFound},
		{"/", http.StatusNotFound},
		{"/api/todos", http.StatusOK},
		{"/health", http.StatusOK},
		{"/static/app.min.css", http.StatusOK},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("GET %s = %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

// TestAPIShapeBootHidesPages proves the wiring, not only the middleware: the
// real composition root, booted with APP_SHAPE=api, answers 404 for a page and
// 200 for health. Red-proof: remove applyShapeGate from buildChiMux and /todo
// stops being a 404.
func TestAPIShapeBootHidesPages(t *testing.T) {
	cfg := config.Load()
	cfg.Dev = true
	cfg.SignupEnabled = true
	cfg.RBACEnabled = false
	cfg.DemoAccounts = false
	cfg.AppShape = "api"
	cfg.Turso.DBPath = filepath.Join(t.TempDir(), "api.db")
	cfg.DataDir = filepath.Join(t.TempDir(), "data")
	cfg.NATS.Enabled = false
	cfg.DagNats.Enabled = false
	cfg.GoAkt.Enabled = false
	stack, err := BootChi(cfg)
	if err != nil {
		t.Fatalf("BootChi: %v", err)
	}
	t.Cleanup(func() {
		stack.cancelConsumer()
		for _, stop := range stack.shutdown {
			stop()
		}
		_ = stack.sqlDB.Close()
	})

	cases := []struct {
		path string
		want int
	}{
		{"/todo", http.StatusNotFound},
		{"/dashboard", http.StatusNotFound},
		{"/health", http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		stack.mux.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("GET %s = %d, want %d", tc.path, rec.Code, tc.want)
		}
	}
}

func TestAPIPathAllowedKeepsTheSeparator(t *testing.T) {
	for _, path := range []string{"/api", "/api/todos", "/health", "/static/x", "/_ui/locale", "/sw.js"} {
		if !apiPathAllowed(path) {
			t.Errorf("%q must be reachable in the api shape", path)
		}
	}
	for _, path := range []string{"/apix", "/apifoo", "/staticx", "/todoo", "/console/"} {
		if apiPathAllowed(path) {
			t.Errorf("%q must be a page/operator path in the api shape", path)
		}
	}
}
