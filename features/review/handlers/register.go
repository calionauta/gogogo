// The review slice mounts itself through the recipe seam.
package handlers

import (
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the review handler and hands its declaration to the seam.
// The Runner (Genkit instance + model action + judge) is built here from
// Cfg and the RealLLM client; the composition root passes the same clients
// it hands every other unit.
func Register(reg *recipe.Registry, d register.Deps) {
	if d.RealLLM == nil {
		// No model client: the review page would 503 every run. Register
		// nothing; the route gate sees no claim, same nil-Provider rule as
		// room when its engine is off.
		return
	}
	g := NewGenkit()
	runner := &Runner{
		G:            g,
		Model:        DefineReviewModel(g, d.RealLLM),
		Judge:        NewJudgeClient(d.Cfg),
		Threshold:    d.Cfg.Review.ConfidenceThreshold,
		JudgeTimeout: d.Cfg.Review.Timeout,
	}
	h := NewHandler(d.Cfg, d.Auth, runner, d.RealLLM)
	h.RegisterFlows()
	reg.Add(h.Recipe())
}
