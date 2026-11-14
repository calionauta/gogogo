// The UI affordance for analytics: one declarative attribute, one endpoint.
//
// Client-side tracking exists for what only the browser knows — a click on an
// affordance — and nothing else: domain events travel server-side through the
// same bus (interaction.go). The endpoint is mounted only when a provider is
// configured, so a tree with analytics off has no beacon at all.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/internal/analytics"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/events"
)

const (
	analyticsPath = "/api/analytics"
	// eventUIInteractionPrefix marks a browser-reported affordance. The name
	// after the dot is the allowlisted attribute value.
	eventUIInteractionPrefix = "ui."
	// Limits: a beacon is tiny, and an unbounded body or name is an abuse
	// surface rather than a metric.
	analyticsBodyLimit = 4 << 10
	analyticsPathLimit = 200
)

// analyticsName is the allowlist for an attribute name: lowercase, dotted,
// bounded. Markup contributes data, never executable text.
var analyticsName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,63}$`)

// mountAnalytics registers POST /api/analytics when a provider is configured.
func mountAnalytics(r *Router, a *auth.Auth, cfg *config.Config, bus *events.Bus) {
	if bus == nil || !analytics.Enabled(analyticsProvider(cfg)) {
		return
	}
	r.Claim("core", http.MethodPost, analyticsPath)
	r.Post(analyticsPath, analyticsHandler(a, bus))
}

// analyticsBeacon is the request shape the shell script sends.
type analyticsBeacon struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// analyticsHandler turns one attributed browser interaction into a bus event.
// A signed-out request is dropped quietly: there is no actor to attribute it
// to, and nothing about the session is echoed back.
func analyticsHandler(a *auth.Auth, bus *events.Bus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, err := a.Session(r)
		if err != nil || sess == nil || sess.User == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		body, rerr := io.ReadAll(http.MaxBytesReader(w, r.Body, analyticsBodyLimit))
		if rerr != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if cerr := captureUIEvent(r.Context(), bus, sess.User.ID, body); cerr != nil {
			http.Error(w, "invalid event", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// captureUIEvent validates one beacon and publishes it. Split from the handler
// so the rules (allowlisted name, bounded path, attributed actor) are testable
// without a session, and so a bad payload answers one error, not a 400 per rule.
func captureUIEvent(ctx context.Context, bus *events.Bus, actor string, body []byte) error {
	var beacon analyticsBeacon
	if err := json.Unmarshal(body, &beacon); err != nil {
		return errors.New("analytics: malformed beacon")
	}
	if !analyticsName.MatchString(beacon.Name) {
		return errors.New("analytics: event name not in the allowlist")
	}
	path := beacon.Path
	if len(path) > analyticsPathLimit {
		path = path[:analyticsPathLimit]
	}
	bus.Emit(ctx, events.Event{
		Name: eventUIInteractionPrefix + beacon.Name,
		Fields: map[string]any{
			fieldPath: path,
			"actor":   actor,
		},
	})
	return nil
}
