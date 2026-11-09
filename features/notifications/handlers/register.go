// The inbox slice mounts itself through the recipe seam.
package handlers

import (
	"context"

	"github.com/calionauta/gogogo/features/notifications"
	"github.com/calionauta/gogogo/internal/auth"
	"github.com/calionauta/gogogo/internal/delivery"
	"github.com/calionauta/gogogo/internal/delivery/notify"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// Register builds the inbox handler and hands its declaration to the seam.
// The bus and the handle the root needs (session stream feed, DagNats
// worker) are published through d.InboxRef so the root never imports this
// package (the self-mounting gate).
func Register(reg *recipe.Registry, d register.Deps) {
	bus := notifications.NewBus()
	h := NewHandler(d.Auth, d.Cfg, d.Queries, bus, buildRouter(d))
	reg.Add(h.Recipe())
	if d.InboxRef != nil {
		d.InboxRef.Bus = bus
		d.InboxRef.Deliver = func(ctx context.Context, dd any) error {
			del, ok := dd.(notifications.Delivery)
			if !ok {
				return nil
			}
			return h.Deliver(ctx, del)
		}
		d.InboxRef.SetRunStarter = func(r any) {
			rs, ok := r.(notifications.RunStarter)
			if !ok {
				return
			}
			h.SetRunStarter(rs)
		}
		d.InboxRef.Wired = func() bool { return h.Wired() }
	}
}

// buildRouter resolves the email transport from config. Kept here (not in
// the root) so the unit owns its delivery wiring; "off" returns an empty
// router, which records skipped/provider rather than dropping silently.
func buildRouter(d register.Deps) *delivery.Router {
	switch d.Cfg.Delivery.Transport {
	case "smtp":
		mailer := auth.NewSMTPMailer(d.Cfg.Auth.SMTP.Addr, d.Cfg.Auth.SMTP.From,
			d.Cfg.Auth.SMTP.Username, d.Cfg.Auth.SMTP.Password)
		return delivery.NewRouter(map[delivery.Channel]delivery.Sender{
			delivery.ChannelEmail: delivery.NewFunc("smtp", func(_ context.Context, m delivery.Message) error {
				return mailer(m.To, m.Subject, m.Body)
			}),
		})
	case "notify":
		return delivery.NewRouter(map[delivery.Channel]delivery.Sender{
			delivery.ChannelEmail: notify.New(d.Cfg.Delivery.Notify.URL, d.Cfg.Delivery.Notify.APIKey),
		})
	default:
		return delivery.NewRouter(nil)
	}
}
