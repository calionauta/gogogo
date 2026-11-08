package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/auth"
)

// fakeAuth is nil-safe: auth.Auth.Session on a nil receiver returns error,
// so an unauthenticated request must 401/redirect, never panic.
func TestHandlerRejectsUnauthenticated(t *testing.T) {
	h := &Handler{auth: &auth.Auth{}}
	for _, path := range []string{"/api/graph/panorama", "/api/graph/dagnats", "/api/graph/ping"} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.Routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("GET %s = %d, want 401", path, rec.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("GET %s: not JSON: %v", path, err)
		}
		if body["error"] == "" {
			t.Fatalf("GET %s: missing error field: %v", path, body)
		}
	}
}
