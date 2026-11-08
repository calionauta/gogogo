package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func localeCookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   31536000,
	}
}

func TestResolvePrefersQueryOverCookie(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo?lang=pt-BR", nil)
	r.AddCookie(localeCookie("en"))
	if got := Resolve(r, []string{"en", "pt-BR"}, "en"); got != "pt-BR" {
		t.Fatalf("query must win, got %q", got)
	}
}

func TestResolveUsesCookieThenAcceptLanguage(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo", nil)
	r.AddCookie(localeCookie("pt-BR"))
	if got := Resolve(r, []string{"en", "pt-BR"}, "en"); got != "pt-BR" {
		t.Fatalf("cookie must win, got %q", got)
	}
	r2 := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo", nil)
	r2.Header.Set("Accept-Language", "fr;q=0.9, pt-BR;q=0.8, en;q=0.7")
	if got := Resolve(r2, []string{"en", "pt-BR"}, "en"); got != "pt-BR" {
		t.Fatalf("Accept-Language must match pt-BR, got %q", got)
	}
}

func TestResolveIgnoresUnsupported(t *testing.T) {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo?lang=de", nil)
	r.AddCookie(localeCookie("fr"))
	r.Header.Set("Accept-Language", "es")
	if got := Resolve(r, []string{"en", "pt-BR"}, "en"); got != "en" {
		t.Fatalf("unsupported must fall back, got %q", got)
	}
}

func TestParseSupported(t *testing.T) {
	got, err := ParseSupported("en, pt-BR , en")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "en" || got[1] != "pt-BR" {
		t.Fatalf("ParseSupported = %v", got)
	}
	if _, err := ParseSupported("!!!"); err == nil || err.Error() == "" {
		t.Fatal("invalid tag must error")
	}
}
