// Analytics as a provider seam, not a vendor script in every template.
//
// The bus already carries domain events; an analytics provider is one sink.
// The default is none. PostHog is the first implementation because its capture
// API is a single server-side POST — no browser script, no GTM container, no
// second CSP surface.
package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/calionauta/gogogo/internal/events"
	"github.com/calionauta/gogogo/internal/observability"
)

// Provider captures one event.
type Provider interface {
	Capture(ctx context.Context, event events.Event)
}

// Noop is the default: no network, no cost.
type Noop struct{}

// Capture implements Provider.
func (Noop) Capture(context.Context, events.Event) {}

// PostHog posts to the project's /capture/ endpoint. Key is the project API
// key; Host defaults to https://app.posthog.com.
type PostHog struct {
	Host   string
	Key    string
	Client *http.Client
}

// Capture implements Provider.
func (p *PostHog) Capture(ctx context.Context, event events.Event) {
	if p == nil || p.Key == "" {
		return
	}
	host := strings.TrimRight(p.Host, "/")
	if host == "" {
		host = "https://app.posthog.com"
	}
	distinct := "anonymous"
	if v, ok := event.Fields["actor"].(string); ok && v != "" {
		distinct = v
	}
	payload := map[string]any{
		"api_key":     p.Key,
		"event":       event.Name,
		"distinct_id": distinct,
		"properties":  event.Fields,
		"timestamp":   event.At.UTC().Format(time.RFC3339Nano),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/capture/", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		observability.Warn(ctx, "analytics: posthog capture failed",
			append(observability.WithComponent("analytics", "servant"),
				"event", event.Name, "error", err)...)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		observability.Warn(ctx, "analytics: posthog capture status",
			append(observability.WithComponent("analytics", "servant"),
				"event", event.Name, "status", resp.StatusCode)...)
	}
}

// Sink adapts a Provider to the events bus.
type Sink struct{ Provider Provider }

// Deliver implements events.Sink.
func (s Sink) Deliver(ctx context.Context, event events.Event) {
	if s.Provider == nil {
		return
	}
	s.Provider.Capture(ctx, event)
}

// FromConfig builds the configured provider. Unknown or keyless providers fall
// back to Noop, so a typo never sends events to the wrong place.
func FromConfig(provider, key, host string) Provider {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "posthog":
		if strings.TrimSpace(key) == "" {
			return Noop{}
		}
		return &PostHog{Host: host, Key: key}
	case "", "none":
		return Noop{}
	default:
		return Noop{}
	}
}

// Enabled reports whether a provider will actually send.
func Enabled(p Provider) bool {
	_, noop := p.(Noop)
	if p == nil || noop {
		return false
	}
	return true
}
