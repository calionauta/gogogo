// Single source of truth for the kiosk's decision engine
// (KIOSK_CLASSIFIER_* env). Split from config.go the way review.go is.
package config

// KioskClassifierConfig configures the decision engine that settles an order
// against the menu.
//
// It is not a fallback: the deterministic parser is the engine's fast path (a
// phrase the closed menu already matches is answered without a round trip), and
// the classifier decides whatever the menu could not. Both produce an ID from
// the menu; neither invents one.
//
// The endpoint, model, key and timeout are not duplicated here: the engine reads
// the same JEV_* settings the review judge and the Chat router use, so one
// primary→fallback chain serves every jev-like call in the app. A second copy of
// an endpoint is a calibration trap and a second place for an operator to look.
//
// ON by default: a kiosk exists to be talked to, and an utterance the menu
// cannot settle is the normal case, not the exception. The engine only runs when
// the parser left something open, so a plain "large latte" still costs no round
// trip. Enabled=false is the opt-out for an offline counter — no client is
// built, no HTTP call is made, and the parse is byte-identical.
type KioskClassifierConfig struct {
	// Enabled is KIOSK_CLASSIFIER_ENABLED (default true).
	Enabled bool
	// ConfidenceThreshold is the confidence below which the engine's choice is
	// ignored and the parser's "missing" stands
	// (KIOSK_CLASSIFIER_CONFIDENCE_THRESHOLD, 0..1). It matches the review
	// judge's escalation threshold: "when is a classifier's choice not good
	// enough to act on alone" is one question with one answer.
	ConfidenceThreshold float64
}

// DefaultKioskClassifierConfidenceThreshold is the confidence below which the
// kiosk ignores the engine's choice.
const DefaultKioskClassifierConfidenceThreshold = DefaultReviewConfidenceThreshold

// loadKioskClassifierEnv fills KioskClassifier from env. An out-of-range
// threshold resets to the default (fail-safe, never fail-closed), the same shape
// loadChatRouterEnv uses.
func (c *Config) loadKioskClassifierEnv() {
	c.KioskClassifier.Enabled = envBool("KIOSK_CLASSIFIER_ENABLED", true)
	c.KioskClassifier.ConfidenceThreshold = envFloat(
		"KIOSK_CLASSIFIER_CONFIDENCE_THRESHOLD", DefaultKioskClassifierConfidenceThreshold)
	if c.KioskClassifier.ConfidenceThreshold < 0 || c.KioskClassifier.ConfidenceThreshold > 1 {
		c.KioskClassifier.ConfidenceThreshold = DefaultKioskClassifierConfidenceThreshold
	}
}
