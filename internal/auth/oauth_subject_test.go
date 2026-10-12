// SCOPE:core — Auth OAuth subject tests (sub-keying, legacy adoption).

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
)

// oauthCallback runs one stub-provider login round-trip, returning the
// callback status. One helper so the subject tests read as flows, not setup.
func oauthCallback(t *testing.T, mux *http.ServeMux) int {
	t.Helper()
	start := httptest.NewRecorder()
	mux.ServeHTTP(start, httptest.NewRequestWithContext(context.Background(),
		http.MethodGet, "/api/auth/oauth/stub", nil))
	if start.Code != http.StatusFound {
		t.Fatalf("start status = %d, want 302", start.Code)
	}
	loc, err := url.Parse(start.Header().Get("Location"))
	if err != nil {
		t.Fatalf("start location: %v", err)
	}
	cb := httptest.NewRecorder()
	mux.ServeHTTP(cb, httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/auth/oauth/stub/callback?code=stub-code&state="+loc.Query().Get("state"), nil))
	return cb.Code
}

func subProvider(idpURL string) OAuthProvider {
	return OAuthProvider{
		Name: "stub", ClientID: "id", ClientSecret: "secret",
		RedirectURL:  "http://x.test/api/auth/oauth/stub/callback",
		AuthURL:      idpURL + "/auth",
		TokenURL:     idpURL + "/token",
		UserInfoURL:  idpURL + "/userinfo",
		Scopes:       []string{"email"},
		ParseEmail:   parseGoogleEmail,
		ParseSubject: parseGoogleSubject,
	}
}

// TestOAuthSubjectSurvivesEmailChange pins the sub-keyed link: the same
// provider subject with a recycled address logs into the SAME user (no
// duplicate row, no takeover). Delete the subject lookup and the second
// login provisions a stranger holding the victim's old address.
func TestOAuthSubjectSurvivesEmailChange(t *testing.T) {
	ctx := context.Background()
	a := pluginAuth(t)
	idp := stubIDP(t, "first@example.com", true)
	a.SetOAuthProviders(map[string]OAuthProvider{"stub": subProvider(idp.URL)})
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())

	if code := oauthCallback(t, mux); code != http.StatusSeeOther {
		t.Fatalf("first callback status = %d", code)
	}
	// Same subject, recycled address (the provider gave first@example.com
	// to a new human): still exactly one user, and it is the original.
	idp2 := stubIDP(t, "second-owner@example.com", true)
	a.SetOAuthProviders(map[string]OAuthProvider{"stub": subProvider(idp2.URL)})
	if code := oauthCallback(t, mux); code != http.StatusSeeOther {
		t.Fatalf("second callback status = %d", code)
	}
	n, err := a.q.CountUsers(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("users = %d, want 1 (subject link, not email link)", n)
	}
	if _, err := a.q.GetOAuthAccount(ctx, dbgen.GetOAuthAccountParams{
		Provider: "stub", Subject: "stub:stub-sub-1",
	}); err != nil {
		t.Fatalf("sub-keyed link missing: %v", err)
	}
}

// TestOAuthUnverifiedEmailRejected pins the verified claim at the HTTP
// layer: a provider response with email_verified=false fails the login
// (502 from the callback), so an attacker-controlled IdP response without
// a verified address never provisions.
func TestOAuthUnverifiedEmailRejected(t *testing.T) {
	a := pluginAuth(t)
	idp := stubIDP(t, "unverified@example.com", false)
	a.SetOAuthProviders(map[string]OAuthProvider{"stub": subProvider(idp.URL)})
	mux := http.NewServeMux()
	mux.Handle("/api/auth/", a.Handler())
	if code := oauthCallback(t, mux); code != http.StatusBadGateway {
		t.Fatalf("unverified callback status = %d, want 502", code)
	}
}

// TestOAuthSubjectRequired pins the fail-closed provider contract: without
// a subject extractor there is no link key, so identity resolution errors
// instead of falling back to an email-derived subject (the takeover the
// sub-keying exists to prevent).
func TestOAuthSubjectRequired(t *testing.T) {
	idp := stubIDP(t, "nosub@example.com", true)
	p := subProvider(idp.URL)
	p.ParseSubject = nil
	if _, _, err := fetchOAuthIdentity(context.Background(), p, "stub-access"); err == nil {
		t.Fatal("nil subject extractor accepted (must fail, never email-fallback)")
	}
}

// TestGitHubSubjectParses pins the /user id extraction (and its absence).
func TestGitHubSubjectParses(t *testing.T) {
	sub, err := parseGitHubSubject([]byte(`{"id":12345,"login":"octo"}`))
	if err != nil || sub != "12345" {
		t.Fatalf("subject = %q, %v; want 12345, nil", sub, err)
	}
	if _, err := parseGitHubSubject([]byte(`{"login":"octo"}`)); err == nil {
		t.Fatal("missing id accepted")
	}
	if _, err := parseGoogleSubject([]byte(`{"email":"a@b.c"}`)); err == nil {
		t.Fatal("missing sub accepted")
	}
}
