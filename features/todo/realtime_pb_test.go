package todo_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
	"time"
)

// TestLoginIssuesPbAuthCookie is the regression guard for the (a)
// prototype's server-half. Login must issue BOTH the app's gogogo_auth
// cookie AND PocketBase's native pb_auth cookie (same PB token, two
// names). pb_auth is what lets a browser's /api/realtime SSE channel
// authenticate as the user so PocketBase delivers todos record-change
// events (per-user scoped). Without it PB realtime is unauthenticated and
// PocketBase's per-subscriber record-access check silently drops every
// record event — that was the blocker that made PB realtime a non-drop-in.
//
// The full PB realtime delivery (subscribe + create → event arrives) is
// PocketBase's standard behavior once pb_auth is present; it is validated
// at the integration level (the running server authenticates the
// /api/realtime connection and PB fans out record changes to the
// authed subscriber). This test guards the bridge that enables it.
func TestLoginIssuesPbAuthCookie(t *testing.T) {
	t.Parallel()
	base, _, _, _, cleanup := testFixture(t)
	defer cleanup()
	ctx := context.Background()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	httpClient := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	loginUser(ctx, t, httpClient, base, demoEmail, demoPassword)

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("url.Parse(base): %v", err)
	}
	var gogogo, pb string
	for _, c := range jar.Cookies(u) {
		switch c.Name {
		case "gogogo_auth":
			gogogo = c.Value
		case "pb_auth":
			pb = c.Value
		}
	}
	if gogogo == "" {
		t.Fatalf("login did not issue gogogo_auth")
	}
	if pb == "" {
		t.Fatalf("login did not issue pb_auth (the (a) bridge is missing)")
	}
	if pb != gogogo {
		t.Fatalf("pb_auth and gogogo_auth must carry the same PB token (pb_auth=%q gogogo_auth=%q)", pb, gogogo)
	}
}
