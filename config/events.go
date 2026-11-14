// Hooks, outbound webhooks and analytics transport.
//
// The bus is in-process and non-blocking; a durable outbox is the next step
// (documented in docs/hooks-analytics.md). The config is opt-in: no URL, no
// provider, no events leaving the binary.
package config

import (
	"log/slog"
	"strings"
)

// EventsConfig holds the hook/analytics transport settings.
type EventsConfig struct {
	// WebhookURL is EVENTS_WEBHOOK_URL. Empty disables outbound webhooks.
	WebhookURL string
	// WebhookSecret is EVENTS_WEBHOOK_SECRET, used for the HMAC signature.
	WebhookSecret string
	// AnalyticsProvider is ANALYTICS_PROVIDER: none (default) or posthog.
	AnalyticsProvider string
	// AnalyticsKey is ANALYTICS_KEY (PostHog project API key).
	AnalyticsKey string
	// AnalyticsHost is ANALYTICS_HOST (default https://app.posthog.com).
	AnalyticsHost string
}

// loadEventsEnv fills Events from env.
func (c *Config) loadEventsEnv() {
	c.Events.WebhookURL = strings.TrimSpace(getEnv("EVENTS_WEBHOOK_URL", ""))
	c.Events.WebhookSecret = strings.TrimSpace(getEnv("EVENTS_WEBHOOK_SECRET", ""))
	c.Events.AnalyticsKey = strings.TrimSpace(getEnv("ANALYTICS_KEY", ""))
	c.Events.AnalyticsHost = strings.TrimSpace(getEnv("ANALYTICS_HOST", "https://app.posthog.com"))
	if c.Events.AnalyticsHost == "" {
		c.Events.AnalyticsHost = "https://app.posthog.com"
	}
	provider := strings.ToLower(strings.TrimSpace(getEnv("ANALYTICS_PROVIDER", "none")))
	switch provider {
	case "none", "posthog":
		c.Events.AnalyticsProvider = provider
	default:
		slog.Warn("config: unknown ANALYTICS_PROVIDER, using none", "provider", provider)
		c.Events.AnalyticsProvider = "none"
	}
}
