// The room slice mounts itself through the recipe seam.
package handlers

import (
	"context"
	"time"

	appgoakt "github.com/calionauta/gogogo/internal/goakt"
	"github.com/calionauta/gogogo/internal/observability"
	"github.com/calionauta/gogogo/internal/queue"
	"github.com/calionauta/gogogo/internal/recipe"
	"github.com/calionauta/gogogo/internal/recipe/register"
)

// roomShutdownTimeout bounds the host stop during shutdown.
const roomShutdownTimeout = 5 * time.Second

// Register builds the room handler and hands its declaration to the seam.
func Register(reg *recipe.Registry, d register.Deps) {
	if !d.Cfg.GoAkt.Enabled {
		return
	}
	host := appgoakt.New()
	ctx := context.Background()
	if herr := host.Start(ctx); herr != nil {
		observability.Warn(ctx, "server: goakt failed to start, room demo disabled", "error", herr)
		return
	}
	appgoakt.SetDefault(host)
	if d.OnShutdown != nil {
		rh := host
		d.OnShutdown(func() {
			sctx, cancel := context.WithTimeout(context.Background(), roomShutdownTimeout)
			defer cancel()
			_ = rh.Stop(sctx)
		})
	}
	hub := d.Hub
	if hub == nil {
		hub = queue.NewSSEHub()
	}
	reg.Add(NewHandler(d.Cfg, d.Auth, host, hub).Recipe())
}
