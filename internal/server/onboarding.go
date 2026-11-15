// Onboarding boot and mount: the DagNats stepper is optional, so its wiring
// lives here rather than in chi.go (which is at the file-size gate). Both
// halves are no-ops when the engine is disabled.

package server

import (
	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/onboarding"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// bootOnboarding wires the DagNats onboarding slice onto the chi stack:
// its stepper publishes to the chi hub, its route is served, and the todo
// create path resumes the paused run through the resumer interface.
// Returns nil (no shutdown hooks) when the engine is disabled, so a stock
// boot never registers the route and the resume hook stays off. Split from
// BootChi to keep boot under the length gate, mirroring bootCredits.
func bootOnboarding(
	cfg *config.Config, a *auth.Auth, ref *register.TodoRef, hub *queue.SSEHub,
) (*onboarding.Handler, []func()) {
	if !cfg.DagNats.Enabled {
		return nil, nil
	}
	h := onboarding.NewHandler(a, cfg.DagNats.HTTPAddr, hub)
	if ref != nil && ref.SetOnboarding != nil {
		ref.SetOnboarding(h)
	}
	return h, []func(){h.Shutdown}
}

// mountOnboarding registers the onboarding routes after the composition, not
// inside it: buildChiMux takes one concrete pointer per feature, and every extra
// parameter is a line every trim must edit. Nil is a no-op, so a stock boot
// never registers the route.
func mountOnboarding(r *Router, onboardingH *onboarding.Handler) {
	if onboardingH != nil {
		r.MountFeature("onboarding", onboardingH.Routes())
	}
}
