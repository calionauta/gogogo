// SCOPE:core — Auth route-table contract: methods and provider params.
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRoutesAnswer405AtTheRouter pins the move: the method decision belongs to
// the router. Handlers no longer carry an `if r.Method !=` guard, so this is
// the only thing keeping a GET on a POST endpoint from reaching the handler.
func TestRoutesAnswer405AtTheRouter(t *testing.T) {
	r := (&Auth{}).Routes()
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/auth/signup/credential"},
		{http.MethodGet, "/api/auth/signin/credential"},
		{http.MethodPost, "/api/auth/magic"},
		{http.MethodPut, "/api/auth/signout"},
		{http.MethodPost, "/api/auth/oauth/github"},
		{http.MethodGet, "/api/auth/oauth/github/callback/extra"},
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, nil))
		want := http.StatusMethodNotAllowed
		if tc.method == http.MethodGet && tc.path == "/api/auth/oauth/github/callback/extra" {
			want = http.StatusNotFound // no route of that shape, not a method issue
		}
		if rec.Code != want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, want)
		}
	}
}
