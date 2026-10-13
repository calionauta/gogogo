// Single source of truth for the review-loop judge settings (JEV_*
// env). Split from config.go for the file-size gate (500 lines); the
// Config struct keeps one Review field, loaded via loadReviewEnv.
package config

import (
	"os"
	"strconv"
	"time"
)

// ReviewConfig holds the jev-compatible judge settings consumed by
// features/review. Every field has a working default (the keyless
// SimpleJev demo); operators override via env, and downloaders repoint
// at any jev-compatible API without code changes.
type ReviewConfig struct {
	// APIURL is the judge endpoint (JEV_API_URL). Bare hosts expand via
	// jev.Endpoint to /v1/systemone; URLs with a path are used as-given.
	APIURL string
	// Model is the judge model ID (JEV_MODEL).
	Model string
	// APIKey is the judge credential (JEV_API_KEY, optional). Empty
	// sends no Authorization header, which suits the keyless demo.
	// Operator keys live in the age-encrypted secrets file, never in git.
	APIKey string
	// Timeout bounds one judge HTTP attempt (JEV_TIMEOUT).
	Timeout time.Duration
	// ConfidenceThreshold below which a verdict escalates to a human
	// instead of auto-accepting (JEV_CONFIDENCE_THRESHOLD, 0..1).
	ConfidenceThreshold float64
}

// DefaultJevAPIURL is the keyless SimpleJev demo (2k context, 2 RPS).
// Production needs a featherless.ai account; self-hosting works too.
const DefaultJevAPIURL = "https://simple-jev-demo-api.featherless.ai/v1/classifier"

// DefaultJevModel is the classifier served by the demo endpoint.
const DefaultJevModel = "featherless-ai/Qwen3.6-35B-A3B-classifier"

// DefaultReviewConfidenceThreshold is the jev confidence below which a
// verdict escalates to a human. Const, not literal: mnd + shared with
// the review flow default (features/review/chi).
const DefaultReviewConfidenceThreshold = 0.7

// loadReviewEnv fills Review from env, falling back to the demo
// defaults. Out-of-range thresholds reset to default (fail-safe, never
// fail-closed: a typo must not brick the boot).
func (c *Config) loadReviewEnv() {
	c.Review.APIURL = getEnv("JEV_API_URL", DefaultJevAPIURL)
	c.Review.Model = getEnv("JEV_MODEL", DefaultJevModel)
	c.Review.APIKey = os.Getenv("JEV_API_KEY")
	c.Review.Timeout = envDuration("JEV_TIMEOUT", 10*time.Second)
	c.Review.ConfidenceThreshold = envFloat("JEV_CONFIDENCE_THRESHOLD", DefaultReviewConfidenceThreshold)
	if c.Review.ConfidenceThreshold < 0 || c.Review.ConfidenceThreshold > 1 {
		c.Review.ConfidenceThreshold = DefaultReviewConfidenceThreshold
	}
}

// envFloat reads a float env var, falling back to def when unset or
// unparseable. Lives here (not config.go) with its only caller.
func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}
