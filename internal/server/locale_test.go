package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calionauta/gogogo/internal/i18n"
)

// newLocaleRequest builds a GET for the locale endpoint.
func newLocaleRequest(query, referer string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/_ui/locale?"+query, nil)
	if referer != "" {
		r.Header.Set("Referer", referer)
	}
	return r
}

// TestLocaleHandlerSetsTheCookie is the write half of i18n.Resolve: choosing
// Portuguese must leave a cookie the next render reads.
func TestLocaleHandlerSetsTheCookie(t *testing.T) {
	h := localeHandler(i18n.Default())
	w := httptest.NewRecorder()
	h(w, newLocaleRequest("lang=pt-BR&next=/todo", ""))

	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/todo" {
		t.Errorf("Location = %q, want /todo", got)
	}
	cookies := w.Result().Cookies()
	var found *http.Cookie
	for _, c := range cookies {
		if c.Name == i18n.CookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatalf("no %s cookie was set", i18n.CookieName)
	}
	if found.Value != "pt-BR" {
		t.Errorf("cookie = %q, want pt-BR", found.Value)
	}
	if found.HttpOnly {
		t.Error("the locale cookie is HttpOnly; the UI marks the active option from it")
	}
}

// TestLocaleHandlerRefusesAnUnsupportedTag keeps the picker honest: a link that
// appears to work and changes nothing is worse than a refusal.
func TestLocaleHandlerRefusesAnUnsupportedTag(t *testing.T) {
	h := localeHandler(i18n.Default())
	w := httptest.NewRecorder()
	h(w, newLocaleRequest("lang=fr&next=/todo", ""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unsupported tag", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == i18n.CookieName && c.Value != "" {
			t.Errorf("a refused choice still wrote the cookie: %q", c.Value)
		}
	}
}

// TestLocaleHandlerFallsBackToReferer is the normal navigation: the link carries
// no ?next=, so the browser's Referer is where the user returns.
func TestLocaleHandlerFallsBackToReferer(t *testing.T) {
	h := localeHandler(i18n.Default())
	w := httptest.NewRecorder()
	h(w, newLocaleRequest("lang=en", "https://example.test/dashboard?x=1"))
	if got := w.Header().Get("Location"); got != "/dashboard" {
		t.Errorf("Location = %q, want the referer path /dashboard", got)
	}
}

// TestLocaleHandlerRefusesOpenRedirects: ?next= must not carry a host or scheme.
func TestLocaleHandlerRefusesOpenRedirects(t *testing.T) {
	h := localeHandler(i18n.Default())
	for _, next := range []string{"//evil.test/x", "https://evil.test/x", "evil.test"} {
		w := httptest.NewRecorder()
		h(w, newLocaleRequest("lang=en&next="+next, ""))
		if got := w.Header().Get("Location"); got != "/" {
			t.Errorf("next=%q redirected to %q, want /", next, got)
		}
	}
}

// TestLocaleHandlerClearsTheDefaultCookie: choosing the fallback clears the
// preference instead of pinning it, so the browser's own language wins again.
func TestLocaleHandlerClearsTheDefaultCookie(t *testing.T) {
	h := localeHandler(i18n.Default())
	w := httptest.NewRecorder()
	h(w, newLocaleRequest("lang=en&next=/", ""))
	var found *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == i18n.CookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatal("no cookie was cleared")
	}
	if found.Value != "" || found.MaxAge >= 0 {
		t.Errorf("cookie = %+v, want it expired", found)
	}
}
