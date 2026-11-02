package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/calionauta/gogogo/internal/rbac"
)

// assertPolicyCoverage checks every route the composed tree actually serves.
// This is the half a source scan cannot see: a recipe mounted at runtime
// appears here the moment it mounts, with no manifest line to forget (see
// docs/research/extensibility-2026-11.md, phase 1).
func assertPolicyCoverage(t *testing.T, mux *Router) {
	t.Helper()
	problems, err := rbac.RoutePolicyProblems(mux)
	if err != nil {
		t.Fatalf("policy gate: %v", err)
	}
	if len(problems) > 0 {
		t.Errorf("composed routes with no usable policy decision:\n  %s", strings.Join(problems, "\n  "))
	}
}

// TestRuntimeMountedRouteNeedsAPolicyRow is the phase-1 gate from
// docs/research/extensibility-2026-11.md: a route mounted at RUNTIME — what a
// recipe does — must be visible to the RBAC gate.
//
// The source scan in internal/rbac builds its inventory by reading
// `r.Get("/path")` calls, so a recipe's routes never appear there and the gate
// would stay green while losing coverage. This walks the composed tree instead,
// through the same MountFeature seam the boot path uses, and demands that a
// route with no policy row is REPORTED rather than silently skipped.
func TestRuntimeMountedRouteNeedsAPolicyRow(t *testing.T) {
	root := NewRouter()
	probe := chi.NewRouter()
	probe.Get("/recipe-probe", func(http.ResponseWriter, *http.Request) {})
	root.MountFeature("recipe-probe", probe)

	problems, err := rbac.RoutePolicyProblems(root)
	if err != nil {
		t.Fatalf("policy gate: %v", err)
	}
	found := false
	for _, p := range problems {
		if strings.Contains(p, "/recipe-probe") {
			found = true
		}
	}
	if !found {
		t.Fatalf("a runtime-mounted route with no policy row was not reported: %v", problems)
	}
}
