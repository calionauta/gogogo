package server

import (
	"net/http"
	"testing"

	"github.com/calionauta/gogogo/internal/ui"
)

// TestHomeForPrefersTheDashboard pins the resolution order: the dashboard is
// the signed-in home, and an uninstalled dashboard falls back to a destination
// the role can actually open. A hardcoded /dashboard would 404 the root of a
// tree that trimmed it, which is the failure this test exists to prevent.
func TestHomeForPrefersTheDashboard(t *testing.T) {
	r := NewRouter()
	// The inbox is the rail's FIRST destination, so mounting only the dashboard
	// would let an order bug pass. Both are mounted here on purpose: the
	// assertion is that the dashboard wins the tie, not that it is the only
	// thing served.
	inbox := ui.Visible("user")[0].URL
	r.Claim("core", http.MethodGet, inbox)
	r.Claim("core", http.MethodGet, "/dashboard")

	if got := homeFor("user", r); got != "/dashboard" {
		t.Fatalf("homeFor(user) = %q, want /dashboard even with %q mounted", got, inbox)
	}
}

// TestHomeForFallsBackToAVisibleDestination proves the degraded case: with no
// dashboard mounted, the root goes to something the rail offers this role.
func TestHomeForFallsBackToAVisibleDestination(t *testing.T) {
	r := NewRouter()
	first := ui.Visible("user")[0]
	r.Claim("core", http.MethodGet, first.URL)

	if got := homeFor("user", r); got != first.URL {
		t.Fatalf("homeFor(user) = %q, want the first visible destination %q", got, first.URL)
	}
}

// TestHomeForLastResortIsTheAccountPage pins the floor: every other surface
// may be uninstalled, but a session still needs somewhere to land.
func TestHomeForLastResortIsTheAccountPage(t *testing.T) {
	if got := homeFor("user", NewRouter()); got != "/account" {
		t.Fatalf("homeFor(user) with nothing mounted = %q, want /account", got)
	}
}
