package i18n

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogFromContextFallsBackToDefault(t *testing.T) {
	ctx := context.Background()
	if got := T(ctx, "home.title", nil); got != "Home" {
		t.Fatalf("no ctx catalog must render English, got %q", got)
	}
	ctx = WithLocale(ctx, "pt-BR")
	if got := T(ctx, "home.title", nil); got != "Início" {
		t.Fatalf("pt-BR via default must render, got %q", got)
	}
}

func TestCatalogMiddlewareResolvesAndInjects(t *testing.T) {
	cat := loadTestCatalog(t)
	var got string
	handler := CatalogMiddleware(cat, []string{"en", "pt-BR"}, "en",
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = T(r.Context(), "home.title", nil)
		}))
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo?lang=pt-BR", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r)
	if got != "Início" {
		t.Fatalf("middleware pt-BR render = %q", got)
	}
	r2 := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r2)
	if got != "Home" {
		t.Fatalf("middleware default render = %q", got)
	}
}

func TestCatalogMiddlewareIgnoresUnsupported(t *testing.T) {
	cat := loadTestCatalog(t)
	var locale string
	handler := CatalogMiddleware(cat, []string{"en", "pt-BR"}, "en",
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			locale = Locale(r.Context())
		}))
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/todo?lang=de", nil)
	handler.ServeHTTP(httptest.NewRecorder(), r)
	if locale != "en" {
		t.Fatalf("unsupported must fall back, got %q", locale)
	}
}
