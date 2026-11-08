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
// features/review. Every field has a working default (THX-02 primary,
// SimpleJev fallback); operators override via env, and downloaders
// repoint at any jev-compatible API without code changes.
type ReviewConfig struct {
	// APIURL is the primary judge endpoint (JEV_API_URL). Bare hosts
	// expand via jev.Endpoint to /v1/systemone; URLs with a path are
	// used as-given.
	APIURL string
	// Model is the primary judge model ID (JEV_MODEL).
	Model string
	// APIKey is the primary judge credential (JEV_API_KEY, optional).
	// Empty sends no Authorization header, which suits the keyless
	// free tier. Operator keys live in the age-encrypted secrets
	// file, never in git.
	APIKey string
	// FallbackAPIURL is the secondary endpoint, tried when the
	// primary fails (JEV_FALLBACK_API_URL). Same expansion rule.
	FallbackAPIURL string
	// FallbackModel is the secondary judge model ID
	// (JEV_FALLBACK_MODEL).
	FallbackModel string
	// FallbackAPIKey is the fallback credential
	// (JEV_FALLBACK_API_KEY, optional). Empty = keyless fallback.
	FallbackAPIKey string
	// Timeout bounds one judge HTTP attempt (JEV_TIMEOUT) — shared by
	// both legs.
	Timeout time.Duration
	// ConfidenceThreshold below which a verdict escalates to a human
	// instead of auto-accepting (JEV_CONFIDENCE_THRESHOLD, 0..1).
	ConfidenceThreshold float64
}

// DefaultThxAPIURL is the primary endpoint: HAL-X THX-02, free tier
// (60 decisions/min/IP without a key). Production wants a HAL-X key
// via JEV_API_KEY.
const DefaultThxAPIURL = "https://api.hal-x.ai/v1/decide"

// DefaultThxModel is the model the primary endpoint serves. THX-02
// rejects the THX-01-only question types (number/excerpt/cite); the
// judge only sends choice, so this is a drop-in.
const DefaultThxModel = "thx-02"

// DefaultFallbackAPIURL is the SimpleJev demo (2k context, 2 RPS):
// fine as a fallback, not for production. Self-hosting works too.
const DefaultFallbackAPIURL = "https://simple-jev-demo-api.featherless.ai/v1/classifier"

// DefaultFallbackModel is the classifier served by the fallback demo.
const DefaultFallbackModel = "featherless-ai/Qwen3.6-35B-A3B-classifier"

// DefaultReviewConfidenceThreshold is the jev confidence below which a
// verdict escalates to a human. Const, not literal: mnd + shared with
// the review flow default (features/review/handlers).
const DefaultReviewConfidenceThreshold = 0.7

// loadReviewEnv fills Review from env, falling back to the primary +
// fallback defaults. Out-of-range thresholds reset to default
// (fail-safe, never fail-closed: a typo must not brick the boot).
func (c *Config) loadReviewEnv() {
	c.Review.APIURL = getEnv("JEV_API_URL", DefaultThxAPIURL)
	c.Review.Model = getEnv("JEV_MODEL", DefaultThxModel)
	c.Review.APIKey = os.Getenv("JEV_API_KEY")
	c.Review.FallbackAPIURL = getEnv("JEV_FALLBACK_API_URL", DefaultFallbackAPIURL)
	c.Review.FallbackModel = getEnv("JEV_FALLBACK_MODEL", DefaultFallbackModel)
	c.Review.FallbackAPIKey = os.Getenv("JEV_FALLBACK_API_KEY")
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
