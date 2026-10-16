// Single source of truth for the optional Ask-AI decision router
// (JEV_ROUTER_* env). Split from config.go the way review.go is: the
// Config struct keeps one AskRouter field.
package config

// AskRouterConfig opts Ask AI into a jev-compatible classifier that
// decides which catalog component an answer leads with.
//
// Off by default, and that default is the compatibility contract: with
// Enabled false no router is constructed, no HTTP call is made, and the
// Ask prompt is byte-identical to the one the catalog alone produced. A
// router that is on but unusable — endpoint down, answer below the
// confidence threshold, a label outside the catalog — falls back to that
// same prompt, so the feature can only add a leading hint, never remove
// a component from what the model may emit.
//
// The endpoint itself is not duplicated here: the router reads the same
// JEV_* settings the review judge uses (see ReviewConfig), so one
// jev-compatible API serves both.
type AskRouterConfig struct {
	// Enabled is JEV_ROUTER_ENABLED (default false).
	Enabled bool
	// ConfidenceThreshold is the classifier confidence below which its
	// pick is ignored and the whole catalog is offered
	// (JEV_ROUTER_CONFIDENCE_THRESHOLD, 0..1).
	ConfidenceThreshold float64
}

// DefaultAskRouterConfidenceThreshold is the confidence below which Ask
// ignores the router's pick. It matches the review judge's escalation
// threshold on purpose: both answer "when is the classifier's choice not
// good enough to act on alone", and two different numbers for one
// endpoint would be a calibration trap.
const DefaultAskRouterConfidenceThreshold = DefaultReviewConfidenceThreshold

// loadAskRouterEnv fills AskRouter from env. An out-of-range threshold
// resets to the default (fail-safe, never fail-closed: a typo must not
// brick the boot), the same shape loadReviewEnv uses.
func (c *Config) loadAskRouterEnv() {
	c.AskRouter.Enabled = envBool("JEV_ROUTER_ENABLED", false)
	c.AskRouter.ConfidenceThreshold = envFloat("JEV_ROUTER_CONFIDENCE_THRESHOLD", DefaultAskRouterConfidenceThreshold)
	if c.AskRouter.ConfidenceThreshold < 0 || c.AskRouter.ConfidenceThreshold > 1 {
		c.AskRouter.ConfidenceThreshold = DefaultAskRouterConfidenceThreshold
	}
}
