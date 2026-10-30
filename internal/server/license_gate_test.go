package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/calionauta/gogogo/internal/license"
)

func activatedStore(t *testing.T) *license.Store {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	st, err := license.Open(filepath.Join(t.TempDir(), "license.token"), pub)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	token, err := license.Sign(priv, license.Claims{Plan: "pro"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := st.Activate(token, time.Now()); err != nil {
		t.Fatalf("activate: %v", err)
	}
	return st
}

// TestLicenseGateBlocksUntilActivated is the gate contract: pages redirect to
// /activate, APIs get JSON 403, and the activation/asset/health allowlist
// passes through so the page itself can render.
func TestLicenseGateBlocksUntilActivated(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	closed, err := license.Open(filepath.Join(t.TempDir(), "license.token"), pub)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	gated := licenseGate(closed, next)

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "page redirects", method: http.MethodGet, path: "/notes", want: http.StatusSeeOther},
		{name: "api is json 403", method: http.MethodPost, path: "/api/todos", want: http.StatusForbidden},
		{name: "stream is 403", method: http.MethodGet, path: "/api/todos/stream", want: http.StatusForbidden},
		{name: "activation page passes", method: http.MethodGet, path: "/activate", want: http.StatusOK},
		{name: "assets pass", method: http.MethodGet, path: "/static/app.min.css", want: http.StatusOK},
		{name: "health passes", method: http.MethodGet, path: "/health", want: http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
			gated.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
			}
			if tc.want == http.StatusSeeOther && rec.Header().Get("Location") != "/activate" {
				t.Fatalf("redirect target = %q", rec.Header().Get("Location"))
			}
		})
	}
}

func TestLicenseGatePassesWhenActivated(t *testing.T) {
	gated := licenseGate(activatedStore(t), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/notes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("activated gate = %d, want 200", rec.Code)
	}
}
