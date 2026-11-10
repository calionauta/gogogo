// The skinned 403: what a refused page answers with.
//
// The decision lives in internal/rbac; the ANSWER is a page. This test
// drives the whole path — the middleware, the injected renderer, the shell —
// so a wiring slip shows up here and not as a bare "forbidden" in a
// browser.
package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/calionauta/gogogo/internal/demo"
)

// TestForbiddenPageRendersTheSkin pins the refusal a signed-in user gets
// when the policy denies the page: 403 with the app's shell around it, not
// a bare body. The status stays 403 (the page is an answer, not a
// redirect), and the page never names what was behind the door.
func TestForbiddenPageRendersTheSkin(t *testing.T) {
	base, signIn := bootDemoStack(t, true)
	cookie := signIn(demo.Email2)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/config", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.AddCookie(cookie)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /config: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer GET /config = %d, want 403", resp.StatusCode)
	}
	page := string(body)
	for _, want := range []string{
		// The shell: the rail, the module header, the account menu.
		`id="app-rail"`, "app-module-header", "app-user-email",
		// The page's own copy.
		"Access denied", "You don&#39;t have access to this page",
		// The way out: two destinations every role may open.
		`href="/dashboard"`, `href="/account"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("forbidden page missing %q", want)
		}
	}
	if strings.Contains(page, "Runtime status") {
		t.Error("forbidden page leaked the refused surface")
	}
}

// TestForbiddenDataRequestKeepsThePlainBody pins the audience split: a
// data client (XHR, Datastar action) gets the status and a short body, not
// a page patched into a live UI.
func TestForbiddenDataRequestKeepsThePlainBody(t *testing.T) {
	base, signIn := bootDemoStack(t, true)
	cookie := signIn(demo.Email2)
	// The inbox API is user+ in the policy: a viewer is refused it.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/api/notifications/badge", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.AddCookie(cookie)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/notifications/badge: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer GET /api/notifications/badge = %d, want 403", resp.StatusCode)
	}
	if strings.Contains(string(body), "app-rail") {
		t.Error("a data request got the HTML page; it wants the plain body")
	}
}
