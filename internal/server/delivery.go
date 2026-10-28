// SCOPE:core — delivery transport wiring (composition root).
//
// The feature depends on delivery.Sender, never on a provider package; this
// file is the only place that names one. Config validation refused an
// impossible combination at boot, so a Sender built here is always complete.
package server

import (
	"context"

	"github.com/calionauta/gogogo/config"
	"github.com/calionauta/gogogo/features/notifications"
	notificationshandlers "github.com/calionauta/gogogo/features/notifications/handlers"
	"github.com/calionauta/gogogo/internal/auth"
	dbgen "github.com/calionauta/gogogo/internal/db/sqlcgen"
	"github.com/calionauta/gogogo/internal/delivery"
	"github.com/calionauta/gogogo/internal/delivery/notify"
)

// buildDelivery resolves the email transport. "off" (the default) returns an
// empty router, which the feature records as skipped/provider rather than
// silently dropping the message.
func buildDelivery(cfg *config.Config) *delivery.Router {
	switch cfg.Delivery.Transport {
	case "smtp":
		// Reuse the auth stack's SMTP implementation instead of copying the
		// protocol: the adapter takes a function, so delivery never imports
		// auth.
		mailer := auth.NewSMTPMailer(cfg.Auth.SMTP.Addr, cfg.Auth.SMTP.From,
			cfg.Auth.SMTP.Username, cfg.Auth.SMTP.Password)
		return delivery.NewRouter(map[delivery.Channel]delivery.Sender{
			delivery.ChannelEmail: delivery.NewFunc("smtp", func(_ context.Context, m delivery.Message) error {
				return mailer(m.To, m.Subject, m.Body)
			}),
		})
	case "notify":
		return delivery.NewRouter(map[delivery.Channel]delivery.Sender{
			delivery.ChannelEmail: notify.New(cfg.Delivery.Notify.URL, cfg.Delivery.Notify.APIKey),
		})
	default:
		return delivery.NewRouter(nil)
	}
}

// mountInbox mounts the inbox slice and wires its bus into the browser's
// leader session stream, so the rail badge hears about a delivery without a
// second connection per page. The two go together: without the inbox there is
// no feed, and without the feed the stream stays session-only.
func mountInbox(
	mux *Router, a *auth.Auth, cfg *config.Config, q *dbgen.Queries,
) *notificationshandlers.Handler {
	h, bus := mountNotifications(mux, a, cfg, q)
	a.SetSessionStreamExtras(inboxShellFeed{bus: bus, q: q})
	return h
}

// mountNotifications builds and mounts the inbox slice. It returns the bus as
// well as the handler: the composition root wires the same bus into the
// session stream, so the rail badge hears about a delivery without a second
// connection per page.
func mountNotifications(
	mux *Router, a *auth.Auth, cfg *config.Config, q *dbgen.Queries,
) (*notificationshandlers.Handler, *notifications.Bus) {
	bus := notifications.NewBus()
	notificationsH := notificationshandlers.NewHandler(a, cfg, q, bus, buildDelivery(cfg))
	mux.MountFeature("notifications", notificationsH.Routes())
	return notificationsH, bus
}
