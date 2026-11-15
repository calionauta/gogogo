// Boot-time wiring for the domain-event bus: the sinks a deployment opted into.
//
// Split from chi.go for the file-size gate. Nothing here is on by default: no
// EVENTS_WEBHOOK_URL and no ANALYTICS_PROVIDER means the bus has no sink and no
// event leaves the process.
package server

import (
	"log/slog"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/analytics"
	"github.com/calionauta/gogogo/internal/events"
)

// eventBuffer bounds the bus queue. Small on purpose: when it fills, events
// are dropped with a warning rather than delaying a request, and 64 is enough
// for a burst of writes that a fast receiver drains immediately.
const eventBuffer = 64

// bootEventSinks attaches the configured outbound sinks. The URL is never
// logged: a webhook URL can carry a token in its path.
func bootEventSinks(cfg *config.Config, bus *events.Bus) {
	if cfg == nil || bus == nil {
		return
	}
	if cfg.Events.WebhookURL != "" {
		bus.AddSink(&events.Webhook{
			URL:    cfg.Events.WebhookURL,
			Secret: cfg.Events.WebhookSecret,
		})
		slog.Info("server: outbound webhook wired", "hmac", cfg.Events.WebhookSecret != "")
	}
	if provider := analyticsProvider(cfg); analytics.Enabled(provider) {
		bus.AddSink(analytics.Sink{Provider: provider})
		slog.Info("server: analytics provider wired", "provider", cfg.Events.AnalyticsProvider)
	}
}

// analyticsProvider builds the configured provider once, so the sink and the
// UI beacon cannot disagree about whether analytics is on.
func analyticsProvider(cfg *config.Config) analytics.Provider {
	if cfg == nil {
		return analytics.Noop{}
	}
	return analytics.FromConfig(
		cfg.Events.AnalyticsProvider, cfg.Events.AnalyticsKey, cfg.Events.AnalyticsHost)
}
