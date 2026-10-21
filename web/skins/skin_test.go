package skins_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/a-h/templ"

	"github.com/calionauta/gogogo/web/skins"
)

// TestResolve covers the per-request selection the whole app relies on:
// a registered ?skin= wins, an unknown one normalises to DaisyUI, and the
// configured default applies when the request carries no override.
func TestResolve(t *testing.T) {
	const name = "test-resolve-skin"
	skins.Register(skins.Skin{Name: name, Assets: func() templ.Component { return templ.NopComponent }})

	cases := []struct {
		target   string
		fallback string
		want     string
	}{
		{"/?skin=" + name, "daisyui", name},
		{"/?skin=does-not-exist", "daisyui", "daisyui"},
		{"/todo", name, name},
		{"/todo", "does-not-exist", "daisyui"},
	}
	for _, c := range cases {
		r := httptest.NewRequestWithContext(context.Background(), "GET", c.target, nil)
		if got := skins.Resolve(r, c.fallback); got != c.want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c.target, c.fallback, got, c.want)
		}
	}
}

// TestActiveFallsBackWithoutRegistration pins the graceful default: rendering
// a single component package in a test has no app wiring, and that must not
// panic. The fallback is the default bundle.
func TestActiveFallsBackWithoutRegistration(t *testing.T) {
	got := skins.Active("never-registered")
	if got.Name != "daisyui" {
		t.Fatalf("Active fallback name = %q, want daisyui", got.Name)
	}
	if got.Assets == nil {
		t.Fatal("Active fallback has no assets")
	}
}
